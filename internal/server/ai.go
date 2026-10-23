package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-health/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := userFrom(ctx).Email
	c, status := s.core.SelectAI(ctx, email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const aiVoice = `You are a careful, plain-spoken health companion looking at someone's own numbers from their phone and watch. Use only the data given; never invent values. No diagnoses and no medical claims: you may point out patterns and suggest talking to a doctor when something looks off. Be concrete (numbers, days), short and warm. No emojis, no exclamation marks, no motivational filler. Units are as given.`

// dataDigest renders the last days as compact text for the model.
func (s *Server) dataDigest(ctx context.Context, uid int64, from, to string, st store.Settings) string {
	ins, err := s.insights(ctx, uid, from, to)
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Period %s to %s (%d days). Units: %s.\n", from, to, ins["days"], st.Units)
	if trends, ok := ins["trends"].([]trend); ok {
		b.WriteString("Daily averages (vs the period before):\n")
		for _, t := range trends {
			v, unit := metrics.Display(t.Metric, t.Avg, st.Units == "imperial")
			fmt.Fprintf(&b, "- %s: %s %s", t.Metric.Name, fmtMetric(t.Metric, v), unit)
			if t.PrevAvg != 0 {
				fmt.Fprintf(&b, " (%+.1f%%)", t.Change)
			}
			if t.Days > 0 {
				fmt.Fprintf(&b, "; best %s on %s, worst %s on %s", trimNum(t.Best.Value), t.Best.Date, trimNum(t.Worst.Value), t.Worst.Date)
			}
			b.WriteString("\n")
		}
	}
	if wk, ok := ins["workouts"].(map[string]any); ok {
		fmt.Fprintf(&b, "Workouts: %d (%d min), period before %d (%d min).\n", wk["count"], wk["minutes"], wk["prev_count"], wk["prev_minutes"])
		if kinds, ok := wk["by_kind"].([]store.WorkoutKindStat); ok {
			for _, k := range kinds {
				fmt.Fprintf(&b, "- %s: %d × %d min total, %.1f km, %d kcal\n", k.Name, k.Count, k.DurationS/60, k.DistanceM/1000, int(k.EnergyKcal))
			}
		}
	}
	if sl, ok := ins["sleep"].(map[string]any); ok && sl["nights"] != 0 {
		fmt.Fprintf(&b, "Sleep: %v nights, bedtime about %v (±%v min), wake %v (±%v min), asleep %v min on average, regularity %v/100.\n", sl["nights"], sl["bed_avg"], sl["bed_sd_min"], sl["wake_avg"], sl["wake_sd_min"], sl["asleep_avg_min"], sl["score"])
	}
	if cors, ok := ins["correlations"].([]correlation); ok && len(cors) > 0 {
		b.WriteString("Patterns:\n")
		for _, c := range cors {
			b.WriteString("- " + c.Text + "\n")
		}
	}
	if gs, ok := ins["goals"].([]map[string]any); ok && len(gs) > 0 {
		b.WriteString("Goals:\n")
		for _, g := range gs {
			fmt.Fprintf(&b, "- %s %s %v: met %v of %v days, streak %v (best %v)\n", g["name"], g["direction"], g["target"], g["met"], g["days"], g["streak"], g["best"])
		}
	}
	if wd, ok := ins["weather"].(map[string]any); ok && wd != nil {
		if rows, ok := wd["by_rain"].([]map[string]any); ok {
			for _, r := range rows {
				j, _ := json.Marshal(r)
				b.WriteString("Weather " + string(j) + "\n")
			}
		}
	}
	meds, _ := s.store.ListMedications(ctx, uid, false)
	if len(meds) > 0 {
		var names []string
		for _, m := range meds {
			names = append(names, m.Name)
		}
		b.WriteString("Medications: " + strings.Join(names, ", ") + ".\n")
	}
	syms, _ := s.store.ListSymptoms(ctx, uid, from, to, 50)
	if len(syms) > 0 {
		b.WriteString("Symptoms logged:\n")
		for _, x := range syms {
			fmt.Fprintf(&b, "- %s %s (severity %d/5)\n", x.Date, x.Name, x.Severity)
		}
	}
	return b.String()
}

// handleRecap writes a weekly (or ranged) recap with the person's AI.
func (s *Server) handleRecap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Range string `json:"range"`
	}
	_ = decode(r, &in)
	days := rangeDays(firstNonEmpty(in.Range, "w"))
	to := store.Today()
	from := store.AddDays(to, -(days - 1))
	digest := s.dataDigest(ctx, uid, from, to, st)
	if strings.TrimSpace(digest) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "not enough data yet"})
		return
	}
	prompt := "Write a recap of this period in 4–8 short sentences: what went well, what slipped, one pattern worth knowing and one small, concrete suggestion for next week. Then a line 'Numbers:' with the three most telling figures.\n\n" + digest
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "from": from, "to": to, "text": strings.TrimSpace(text)})
}

// handleAsk answers a question about the person's data.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Question string `json:"question"`
		Range    string `json:"range"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		writeErr(w, http.StatusBadRequest, "question is required")
		return
	}
	days := rangeDays(firstNonEmpty(in.Range, "m"))
	to := store.Today()
	from := store.AddDays(to, -(days - 1))
	digest := s.dataDigest(ctx, uid, from, to, st)
	prompt := "Question: " + strings.TrimSpace(in.Question) + "\n\nTheir data:\n" + digest + "\nAnswer in a few sentences, with the numbers that back it. If the data cannot answer it, say so."
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text)})
}
