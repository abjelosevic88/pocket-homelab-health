package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// medReminders runs every minute: a dose whose time has come (and up to 90
// minutes past) and is not logged gets one push.
func (s *Server) medReminders(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	today := store.Today()
	clock := now.Format("15:04")
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || !st.NotifyMeds || st.NotifyURL == "" {
			continue
		}
		meds, err := s.store.MedsForDay(ctx, uid, today)
		if err != nil {
			continue
		}
		n := notify.New(st.NotifyURL, st.NotifyFormat)
		for _, m := range meds {
			if !m.Remind {
				continue
			}
			for _, sl := range m.Slots {
				if sl.Time == "" || sl.Status != "" || sl.Time > clock {
					continue
				}
				t, _ := time.ParseInLocation("15:04", sl.Time, now.Location())
				due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
				if now.Sub(due) > 90*time.Minute || s.store.ReminderSent(ctx, uid, m.ID, today, sl.Time) {
					continue
				}
				body := m.Name
				if m.Dose != "" {
					body += " · " + m.Dose
				}
				body += " at " + sl.Time + "."
				if m.Stock >= 0 && m.Stock <= 5 {
					body += fmt.Sprintf(" %d left.", m.Stock)
				}
				_ = s.store.MarkReminderSent(ctx, uid, m.ID, today, sl.Time)
				if err := n.Send(ctx, notify.Message{Title: "Time for your medication", Body: body, Priority: "warning", Tags: []string{"pill"}, Link: s.cfg.BaseURL + "/medications"}); err != nil {
					s.log.Warn("medication reminder failed", "user", uid, "err", err)
				}
			}
		}
	}
}

// dailyPushes runs every minute and fires once a day at each person's hour:
// the weekly report on their weekday and the "no data from the phone" nudge.
func (s *Server) dailyPushes(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	today := store.Today()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || st.NotifyURL == "" || now.Hour() != st.NotifyHour || now.Minute() > 4 {
			continue
		}
		key := fmt.Sprintf("daily-push:%d:%s", uid, today)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		_ = s.store.CachePut(ctx, key, "1")
		n := notify.New(st.NotifyURL, st.NotifyFormat)
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		if st.NotifyWeekly && wd == st.WeeklyDay {
			if msg, err := s.weeklyReport(ctx, uid, st); err == nil && msg != "" {
				_ = n.Send(ctx, notify.Message{Title: "Your week in Pocket Health", Body: msg, Priority: "info", Tags: []string{"bar_chart"}, Link: s.cfg.BaseURL + "/insights"})
			}
		}
		if st.NotifyInactive {
			last, _ := s.store.LastSync(ctx, uid)
			newest := ""
			for src, at := range last {
				if src != "manual" && at > newest {
					newest = at
				}
			}
			if newest != "" {
				if t, err := time.Parse(time.RFC3339Nano, newest); err == nil && time.Since(t) > 36*time.Hour {
					_ = n.Send(ctx, notify.Message{Title: "No health data lately", Body: fmt.Sprintf("Nothing has arrived from your phone since %s. Open Health Auto Export (or the app that syncs) to catch up.", t.Local().Format("Mon 2 Jan 15:04")), Priority: "warning", Tags: []string{"iphone"}, Link: s.cfg.BaseURL + "/settings/sources"})
				}
			}
		}
	}
}

// weeklyReport writes the plain-text summary of the last 7 days vs the 7 before.
func (s *Server) weeklyReport(ctx context.Context, uid int64, st store.Settings) (string, error) {
	to := store.AddDays(store.Today(), -1)
	from := store.AddDays(to, -6)
	ins, err := s.insights(ctx, uid, from, to)
	if err != nil {
		return "", err
	}
	trends, _ := ins["trends"].([]trend)
	imperial := st.Units == "imperial"
	var lines []string
	pick := map[string]bool{"steps": true, "sleep_asleep": true, "exercise_min": true, "active_energy": true, "resting_hr": true, "hrv": true, "weight": true, "water": true}
	for _, t := range trends {
		if !pick[t.Metric.Key] {
			continue
		}
		v, unit := metrics.Display(t.Metric, t.Avg, imperial)
		line := t.Metric.Name + ": " + fmtMetric(t.Metric, v) + " " + unit
		if t.Metric.Agg == metrics.Sum {
			line += "/day"
		}
		if t.PrevAvg != 0 {
			sign := "+"
			if t.Change < 0 {
				sign = ""
			}
			line += fmt.Sprintf(" (%s%s%% vs the week before)", sign, trimNum(t.Change))
		}
		lines = append(lines, line)
	}
	if wk, ok := ins["workouts"].(map[string]any); ok {
		if c, _ := wk["count"].(int); c > 0 {
			lines = append(lines, fmt.Sprintf("Workouts: %d (%d min)", c, wk["minutes"].(int)))
		}
	}
	if gs, ok := ins["goals"].([]map[string]any); ok && len(gs) > 0 {
		var parts []string
		for _, g := range gs {
			parts = append(parts, fmt.Sprintf("%s %d/7", g["name"], g["met"]))
		}
		lines = append(lines, "Goals: "+strings.Join(parts, ", "))
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n"), nil
}

func fmtMetric(m metrics.Metric, v float64) string {
	if m.Key == "sleep_asleep" || m.Key == "sleep_in_bed" {
		return fmtMin(int(v))
	}
	return trimNum(metrics.Round(m, v))
}

// handleWeeklyNow sends the weekly report right away (Settings → Notifications).
func (s *Server) handleWeeklyNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	msg, err := s.weeklyReport(ctx, uid, st)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if msg == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "not enough data for a report yet"})
		return
	}
	if st.NotifyURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": false, "report": msg})
		return
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Your week in Pocket Health", Body: msg, Priority: "info", Tags: []string{"bar_chart"}, Link: s.cfg.BaseURL + "/insights"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "report": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": true, "report": msg})
}
