package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// goalView is a goal with today's progress and streaks.
type goalView struct {
	store.Goal
	MetricInfo metrics.Metric `json:"metric_info"`
	Today      *float64       `json:"today"`
	Pct        int            `json:"pct"`
	Met        bool           `json:"met"`
	Streak     int            `json:"streak"`
	Best       int            `json:"best"`
	Last30     int            `json:"last30"` // days met in the last 30
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	goals, err := s.store.ListGoals(ctx, uid)
	if err != nil {
		s.fail(w, err, "goals")
		return
	}
	today := store.Today()
	vals, _ := s.store.DayValues(ctx, uid, today)
	out := []goalView{}
	for _, g := range goals {
		v := goalView{Goal: g, MetricInfo: metrics.Get(g.Metric)}
		if p, ok := vals[g.Metric]; ok {
			x := p.Value
			v.Today = &x
			v.Pct = goalPct(g, x)
			v.Met = goalMet(g, x)
		} else if v.MetricInfo.Agg == metrics.Last {
			if p, err := s.store.LatestDaily(ctx, uid, g.Metric); err == nil {
				x := p.Value
				v.Today = &x
				v.Pct = goalPct(g, x)
				v.Met = goalMet(g, x)
			}
		}
		v.Streak, v.Best, _ = s.store.GoalStreak(ctx, uid, g.Metric)
		hits, _ := s.store.GoalHits(ctx, uid, g.Metric, store.AddDays(today, -29), today)
		v.Last30 = len(hits)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func goalMet(g store.Goal, v float64) bool {
	if g.Direction == "at_most" {
		return v <= g.Target
	}
	return v >= g.Target
}

func goalPct(g store.Goal, v float64) int {
	if g.Target <= 0 {
		return 0
	}
	if g.Direction == "at_most" {
		if v <= g.Target {
			return 100
		}
		return int(max(0, 100-(v-g.Target)/g.Target*100))
	}
	return int(min(100, v/g.Target*100))
}

func (s *Server) handleSetGoal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	metric := strings.ToLower(chi.URLParam(r, "metric"))
	var in struct {
		Target    float64 `json:"target"`
		Direction string  `json:"direction"`
		Period    string  `json:"period"`
		Active    *bool   `json:"active"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Target <= 0 {
		writeErr(w, http.StatusBadRequest, "target must be above zero")
		return
	}
	m := metrics.Get(metric)
	if in.Direction == "" {
		in.Direction = m.GoalDir
		if in.Direction == "" {
			in.Direction = "at_least"
		}
	}
	g := store.Goal{Metric: metric, Target: in.Target, Direction: in.Direction, Period: in.Period, Active: true}
	if in.Active != nil {
		g.Active = *in.Active
	}
	if err := s.store.UpsertGoal(ctx, uid, &g); err != nil {
		s.fail(w, err, "goal")
		return
	}
	// re-evaluate the last 30 days against the new target
	pts, _ := s.store.Daily(ctx, uid, metric, store.AddDays(store.Today(), -29), store.Today())
	t := store.Touched{}
	for _, p := range pts {
		t.Merge(store.Touched{metric: {p.Date: true}})
	}
	s.checkGoalsQuiet(ctx, uid, t)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGoal(r.Context(), actorID(r.Context()), strings.ToLower(chi.URLParam(r, "metric"))); err != nil {
		s.fail(w, err, "delete goal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkGoals re-evaluates the daily goals of the touched days, records hits,
// sends the "goal reached" push for today (once) and runs goal habit rules.
func (s *Server) checkGoals(ctx context.Context, uid int64, touched store.Touched) {
	s.evalGoals(ctx, uid, touched, true)
}

// checkGoalsQuiet is the same without pushes (re-evaluating history).
func (s *Server) checkGoalsQuiet(ctx context.Context, uid int64, touched store.Touched) {
	s.evalGoals(ctx, uid, touched, false)
}

func (s *Server) evalGoals(ctx context.Context, uid int64, touched store.Touched, push bool) {
	if len(touched) == 0 {
		return
	}
	goals, err := s.store.ListGoals(ctx, uid)
	if err != nil {
		return
	}
	var st *store.Settings
	for _, g := range goals {
		if !g.Active || g.Period != "day" {
			continue
		}
		dates := touched[g.Metric]
		if len(dates) == 0 {
			continue
		}
		pts, _ := s.store.Daily(ctx, uid, g.Metric, minKey(dates), maxKey(dates))
		byDate := map[string]float64{}
		for _, p := range pts {
			byDate[p.Date] = p.Value
		}
		for d := range dates {
			v, ok := byDate[d]
			if !ok || !goalMet(g, v) {
				_ = s.store.ClearGoalHit(ctx, uid, g.Metric, d)
				continue
			}
			fresh, err := s.store.RecordGoalHit(ctx, uid, g.Metric, d, v)
			if err != nil {
				continue
			}
			if d == store.Today() {
				s.runGoalRules(ctx, uid, g.Metric, d)
			}
			if !fresh || !push || d != store.Today() {
				continue
			}
			if st == nil {
				x, _ := s.store.GetSettings(ctx, uid)
				st = &x
			}
			m := metrics.Get(g.Metric)
			if st.NotifyGoals && st.NotifyURL != "" {
				dv, unit := metrics.Display(m, v, st.Units == "imperial")
				body := fmt.Sprintf("%s: %s %s today (goal %s).", m.Name, trimNum(dv), unit, trimNum(goalDisplay(m, g.Target, st.Units == "imperial")))
				if streak, _, _ := s.store.GoalStreak(ctx, uid, g.Metric); streak > 1 {
					body += fmt.Sprintf(" %d days in a row.", streak)
				}
				n := notify.New(st.NotifyURL, st.NotifyFormat)
				if err := n.Send(ctx, notify.Message{Title: "Goal reached", Body: body, Priority: "success", Tags: []string{"tada"}, Link: s.cfg.BaseURL + "/metrics/" + g.Metric}); err == nil {
					_ = s.store.MarkGoalNotified(ctx, uid, g.Metric, d)
				}
			} else {
				_ = s.store.MarkGoalNotified(ctx, uid, g.Metric, d)
			}
			s.publish(ctx, uid, "health.goal_reached", "goal:"+g.Metric+":"+d, map[string]any{"metric": g.Metric, "date": d, "value": v, "target": g.Target})
		}
	}
}

func goalDisplay(m metrics.Metric, target float64, imperial bool) float64 {
	v, _ := metrics.Display(m, target, imperial)
	return v
}

func trimNum(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	return s
}

func minKey(m map[string]bool) string {
	out := ""
	for k := range m {
		if out == "" || k < out {
			out = k
		}
	}
	return out
}

func maxKey(m map[string]bool) string {
	out := ""
	for k := range m {
		if k > out {
			out = k
		}
	}
	return out
}
