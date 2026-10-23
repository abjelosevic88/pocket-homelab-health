package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-health/internal/ingest"
	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// handleCatalog lists every metric the app knows, categories and workout kinds.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics.Sorted(), "categories": metrics.CategoryNames, "workout_kinds": store.WorkoutKinds, "manual": metrics.ManualKeys()})
}

// metricView is a catalog entry plus what the person has of it.
type metricView struct {
	metrics.Metric
	Days    int         `json:"days"`
	First   string      `json:"first"`
	Last    string      `json:"last"`
	Latest  float64     `json:"latest"`
	Latest2 *float64    `json:"latest2,omitempty"`
	Goal    *store.Goal `json:"goal,omitempty"`
	Spark   []*float64  `json:"spark"`
	Change  *float64    `json:"change,omitempty"` // vs the week before, in percent
}

// handleListMetrics lists the metrics with data (and the catalog ones without, when ?all=1).
func (s *Server) handleListMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	stats, err := s.store.MetricStats(ctx, uid)
	if err != nil {
		s.fail(w, err, "metrics")
		return
	}
	goals, _ := s.store.ListGoals(ctx, uid)
	goalBy := map[string]store.Goal{}
	for _, g := range goals {
		goalBy[g.Metric] = g
	}
	have := map[string]store.MetricStat{}
	for _, st := range stats {
		have[st.Metric] = st
	}
	today := store.Today()
	from := store.AddDays(today, -13)
	out := []metricView{}
	add := func(m metrics.Metric) {
		v := metricView{Metric: m, Spark: []*float64{}}
		if st, ok := have[m.Key]; ok {
			v.Days, v.First, v.Last, v.Latest, v.Latest2 = st.Days, st.First, st.Last, st.Latest, st.Latest2
			pts, _ := s.store.Daily(ctx, uid, m.Key, from, today)
			byDate := map[string]float64{}
			for _, p := range pts {
				byDate[p.Date] = p.Value
			}
			var thisWeek, lastWeek []float64
			for i := 0; i < 14; i++ {
				d := store.AddDays(from, i)
				if val, ok := byDate[d]; ok {
					if i >= 7 {
						vv := val
						v.Spark = append(v.Spark, &vv)
						thisWeek = append(thisWeek, val)
					} else {
						lastWeek = append(lastWeek, val)
					}
				} else if i >= 7 {
					v.Spark = append(v.Spark, nil)
				}
			}
			if a, b := mean(thisWeek), mean(lastWeek); len(thisWeek) > 0 && len(lastWeek) > 0 && b != 0 {
				c := math.Round((a-b)/math.Abs(b)*1000) / 10
				v.Change = &c
			}
		}
		if g, ok := goalBy[m.Key]; ok {
			gg := g
			v.Goal = &gg
		}
		out = append(out, v)
	}
	seen := map[string]bool{}
	for _, m := range metrics.Sorted() {
		if _, ok := have[m.Key]; ok || queryBool(r, "all") {
			add(m)
			seen[m.Key] = true
		}
	}
	for _, st := range stats {
		if !seen[st.Metric] {
			add(metrics.Get(st.Metric))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t / float64(len(v))
}

// rangeDays maps ?range= to a number of days.
func rangeDays(r string) int {
	switch r {
	case "w", "7", "week":
		return 7
	case "2w", "14":
		return 14
	case "m", "30", "month":
		return 30
	case "3m", "90":
		return 90
	case "6m", "180":
		return 180
	case "y", "365", "year":
		return 365
	case "2y":
		return 730
	case "all":
		return 3650
	}
	return 30
}

// handleMetric is one metric's page: daily points for a range, stats, the goal, streaks.
func (s *Server) handleMetric(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	key := chi.URLParam(r, "key")
	m := metrics.Get(key)
	days := rangeDays(r.URL.Query().Get("range"))
	to := r.URL.Query().Get("to")
	if to == "" || !validDate(to) {
		to = store.Today()
	}
	from := store.AddDays(to, -(days - 1))
	if days >= 3650 {
		if f := s.store.FirstDate(ctx, uid); f != "" {
			from = f
		}
	}
	pts, err := s.store.Daily(ctx, uid, key, from, to)
	if err != nil {
		s.fail(w, err, "metric")
		return
	}
	// previous period for the comparison
	prevPts, _ := s.store.Daily(ctx, uid, key, store.AddDays(from, -days), store.AddDays(from, -1))
	vals := make([]float64, 0, len(pts))
	for _, p := range pts {
		vals = append(vals, p.Value)
	}
	prev := make([]float64, 0, len(prevPts))
	for _, p := range prevPts {
		prev = append(prev, p.Value)
	}
	stats := map[string]any{"days": len(pts)}
	if len(vals) > 0 {
		lo, hi := vals[0], vals[0]
		for _, v := range vals {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		stats["avg"], stats["min"], stats["max"], stats["sum"] = metrics.Round(m, mean(vals)), lo, hi, metrics.Round(m, sum(vals))
		stats["first"], stats["last"] = vals[0], vals[len(vals)-1]
		if len(prev) > 0 {
			stats["prev_avg"] = metrics.Round(m, mean(prev))
			if mean(prev) != 0 {
				stats["change_pct"] = math.Round((mean(vals)-mean(prev))/math.Abs(mean(prev))*1000) / 10
			}
		}
		if m.Agg == metrics.Last && len(vals) > 1 {
			// trend per week from a least-squares line
			stats["per_week"] = metrics.Round(m, slopePerDay(pts)*7)
		}
	}
	var goal *store.Goal
	var hits map[string]bool
	cur, best := 0, 0
	if g, err := s.store.GetGoal(ctx, uid, key); err == nil {
		goal = g
		hits, _ = s.store.GoalHits(ctx, uid, key, from, to)
		cur, best, _ = s.store.GoalStreak(ctx, uid, key)
	}
	latest, _ := s.store.LatestDaily(ctx, uid, key)
	recent, _ := s.store.Samples(ctx, uid, store.SampleFilter{Metric: key, Limit: 20, Desc: true})
	weekly := store.WeeklyAverages(pts, m.Agg == metrics.Sum)
	writeJSON(w, http.StatusOK, map[string]any{
		"metric": m, "from": from, "to": to, "points": pts, "weekly": weekly, "stats": stats, "goal": goal, "goal_hits": hits,
		"streak": cur, "best_streak": best, "latest": latest, "recent": recent, "weekday": weekdayPattern(pts, m),
	})
}

func sum(v []float64) float64 {
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t
}

// slopePerDay fits a line through (day index, value).
func slopePerDay(pts []store.DailyPoint) float64 {
	if len(pts) < 2 {
		return 0
	}
	t0, _ := store.ParseDate(pts[0].Date)
	var sx, sy, sxx, sxy float64
	n := float64(len(pts))
	for _, p := range pts {
		t, _ := store.ParseDate(p.Date)
		x := t.Sub(t0).Hours() / 24
		sx += x
		sy += p.Value
		sxx += x * x
		sxy += x * p.Value
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

// weekdayPattern averages a metric per ISO weekday.
func weekdayPattern(pts []store.DailyPoint, m metrics.Metric) []map[string]any {
	sums := make([]float64, 8)
	ns := make([]int, 8)
	for _, p := range pts {
		t, err := store.ParseDate(p.Date)
		if err != nil {
			continue
		}
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		sums[wd] += p.Value
		ns[wd]++
	}
	names := []string{"", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	out := []map[string]any{}
	for wd := 1; wd <= 7; wd++ {
		v := 0.0
		if ns[wd] > 0 {
			v = metrics.Round(m, sums[wd]/float64(ns[wd]))
		}
		out = append(out, map[string]any{"weekday": wd, "name": names[wd], "avg": v, "n": ns[wd]})
	}
	return out
}

// handleMetricSamples lists the raw samples of a metric in a range.
func (s *Server) handleMetricSamples(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := dateRange(r, 30)
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	list, err := s.store.Samples(ctx, actorID(ctx), store.SampleFilter{Metric: chi.URLParam(r, "key"), From: from, To: to, Limit: limit, Desc: true})
	if err != nil {
		s.fail(w, err, "samples")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleLogSample records a measurement by hand:
// {"metric":"weight","value":80.4,"at":"2025-10-23T07:30","unit":"kg","note":""} (value2 for blood pressure).
func (s *Server) handleLogSample(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Metric string   `json:"metric"`
		Value  *float64 `json:"value"`
		Value2 *float64 `json:"value2"`
		At     string   `json:"at"`
		Unit   string   `json:"unit"`
		Note   string   `json:"note"`
		Source string   `json:"source"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Metric = strings.ToLower(strings.TrimSpace(in.Metric))
	if in.Metric == "" || in.Value == nil {
		writeErr(w, http.StatusBadRequest, "metric and value are required")
		return
	}
	m := metrics.Get(in.Metric)
	if m.Pair && in.Value2 == nil {
		writeErr(w, http.StatusBadRequest, "blood pressure needs value (systolic) and value2 (diastolic)")
		return
	}
	at := time.Now()
	if in.At != "" {
		t, ok := ingest.ParseTime(in.At)
		if !ok {
			writeErr(w, http.StatusBadRequest, "at must be a date/time")
			return
		}
		at = t
	}
	if at.After(time.Now().Add(time.Hour)) {
		writeErr(w, http.StatusBadRequest, "cannot record the future")
		return
	}
	v, ok := metrics.Normalize(m, in.Unit, *in.Value)
	if !ok && in.Unit != "" {
		writeErr(w, http.StatusBadRequest, "unknown unit "+in.Unit+" for "+m.Name)
		return
	}
	source := "manual"
	if app := linkApp(ctx); app != "" {
		source = app
	} else if in.Source != "" {
		source = sanitizeSource(in.Source)
	}
	sm := store.Sample{Metric: m.Key, StartAt: ingest.RFC(at), Value: v, Unit: m.Unit, Source: source, Note: strings.TrimSpace(in.Note)}
	if in.Value2 != nil {
		v2, _ := metrics.Normalize(m, in.Unit, *in.Value2)
		sm.Value2 = &v2
	}
	b := &ingest.Batch{Samples: []store.Sample{sm}}
	if _, err := s.applyBatch(ctx, uid, b, ""); err != nil {
		s.fail(w, err, "log")
		return
	}
	saved, _ := s.store.Samples(ctx, uid, store.SampleFilter{Metric: m.Key, Source: source, Limit: 1, Desc: true})
	if len(saved) > 0 {
		writeJSON(w, http.StatusCreated, saved[0])
		return
	}
	writeJSON(w, http.StatusCreated, sm)
}

func (s *Server) handleUpdateSample(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	sm, err := s.store.GetSample(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "sample")
		return
	}
	if app := linkApp(ctx); app != "" && sm.Source != app {
		writeErr(w, http.StatusForbidden, "a linked app may change only what it recorded")
		return
	}
	var in struct {
		Value  *float64 `json:"value"`
		Value2 *float64 `json:"value2"`
		At     string   `json:"at"`
		Note   *string  `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Value != nil {
		sm.Value = *in.Value
	}
	if in.Value2 != nil {
		sm.Value2 = in.Value2
	}
	if in.At != "" {
		t, ok := ingest.ParseTime(in.At)
		if !ok {
			writeErr(w, http.StatusBadRequest, "at must be a date/time")
			return
		}
		sm.StartAt = ingest.RFC(t)
	}
	if in.Note != nil {
		sm.Note = strings.TrimSpace(*in.Note)
	}
	touched, err := s.store.UpdateSample(ctx, uid, sm)
	if err != nil {
		s.fail(w, err, "update sample")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	_ = s.store.Rollup(ctx, uid, touched, st.PreferDevice)
	s.checkGoals(ctx, uid, touched)
	writeJSON(w, http.StatusOK, sm)
}

func (s *Server) handleDeleteSample(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	touched, err := s.store.DeleteSample(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete sample")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	_ = s.store.Rollup(ctx, uid, touched, st.PreferDevice)
	s.checkGoals(ctx, uid, touched)
	w.WriteHeader(http.StatusNoContent)
}

// ---- Home, Homepage widget, Pocket Day, Pocket Calendar ----

// tile is one number on Home.
type tile struct {
	Metric  metrics.Metric `json:"metric"`
	Value   *float64       `json:"value"`
	Value2  *float64       `json:"value2,omitempty"`
	Date    string         `json:"date"`
	Goal    *store.Goal    `json:"goal,omitempty"`
	Pct     int            `json:"pct"`
	Spark   []*float64     `json:"spark"`
	Streak  int            `json:"streak"`
	IsToday bool           `json:"is_today"`
}

func (s *Server) tilesFor(ctx context.Context, uid int64, keys []string, date string) []tile {
	goals, _ := s.store.ListGoals(ctx, uid)
	goalBy := map[string]store.Goal{}
	for _, g := range goals {
		if g.Active {
			goalBy[g.Metric] = g
		}
	}
	today := s.store.DayValues
	dayVals, _ := today(ctx, uid, date)
	from := store.AddDays(date, -13)
	out := []tile{}
	for _, k := range keys {
		m := metrics.Get(k)
		t := tile{Metric: m, Spark: []*float64{}, IsToday: date == store.Today()}
		if p, ok := dayVals[k]; ok {
			v := p.Value
			t.Value, t.Value2, t.Date = &v, p.Value2, date
		} else if m.Agg == metrics.Last || m.Category == metrics.Body {
			if p, err := s.store.LatestDaily(ctx, uid, k); err == nil {
				v := p.Value
				t.Value, t.Value2, t.Date = &v, p.Value2, p.Date
			}
		}
		pts, _ := s.store.Daily(ctx, uid, k, from, date)
		byDate := map[string]float64{}
		for _, p := range pts {
			byDate[p.Date] = p.Value
		}
		for i := 0; i < 14; i++ {
			d := store.AddDays(from, i)
			if v, ok := byDate[d]; ok {
				vv := v
				t.Spark = append(t.Spark, &vv)
			} else {
				t.Spark = append(t.Spark, nil)
			}
		}
		if g, ok := goalBy[k]; ok {
			gg := g
			t.Goal = &gg
			if t.Value != nil && g.Target > 0 {
				if g.Direction == "at_most" {
					if *t.Value <= g.Target {
						t.Pct = 100
					} else {
						t.Pct = int(math.Max(0, 100-(*t.Value-g.Target)/g.Target*100))
					}
				} else {
					t.Pct = int(math.Min(100, *t.Value/g.Target*100))
				}
			}
			t.Streak, _, _ = s.store.GoalStreak(ctx, uid, k)
		}
		out = append(out, t)
	}
	return out
}

// handleHome is the Today page.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	date := r.URL.Query().Get("date")
	if date == "" || !validDate(date) {
		date = store.Today()
	}
	tiles := s.tilesFor(ctx, uid, st.HomeMetrics, date)
	workouts, _ := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: store.AddDays(date, -6), To: date, Limit: 10})
	nights, _ := s.store.ListSleep(ctx, uid, store.AddDays(date, -6), date, 7)
	meds, _ := s.store.MedsForDay(ctx, uid, date)
	symptoms, _ := s.store.ListSymptoms(ctx, uid, store.AddDays(date, -6), date, 10)
	last, _ := s.store.LastSync(ctx, uid)
	stats, _ := s.store.MetricStats(ctx, uid)
	// a goal summary line
	met, total := 0, 0
	goals, _ := s.store.ListGoals(ctx, uid)
	hitsToday := map[string]bool{}
	for _, g := range goals {
		if !g.Active || g.Period != "day" {
			continue
		}
		total++
		if h, _ := s.store.GoalHits(ctx, uid, g.Metric, date, date); h[date] {
			met++
			hitsToday[g.Metric] = true
		}
	}
	var lastSync string
	for _, at := range last {
		if at > lastSync {
			lastSync = at
		}
	}
	weather, _ := s.store.Weather(ctx, uid, date, date)
	var wd *store.WeatherDay
	if x, ok := weather[date]; ok {
		wd = &x
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date": date, "tiles": tiles, "workouts": workouts, "nights": nights, "medications": meds, "symptoms": symptoms,
		"goals_met": met, "goals_total": total, "goals_hit": hitsToday, "last_sync": lastSync, "has_data": len(stats) > 0, "metrics_count": len(stats), "weather": wd,
		"units": st.Units,
	})
}

// handleDashboard is the Homepage widget: a few numbers.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	today := store.Today()
	vals, _ := s.store.DayValues(ctx, uid, today)
	get := func(k string) float64 {
		if p, ok := vals[k]; ok {
			return p.Value
		}
		return 0
	}
	weight := 0.0
	if p, err := s.store.LatestDaily(ctx, uid, "weight"); err == nil {
		weight = p.Value
	}
	rhr := get("resting_hr")
	if rhr == 0 {
		if p, err := s.store.LatestDaily(ctx, uid, "resting_hr"); err == nil {
			rhr = p.Value
		}
	}
	sleepMin := get("sleep_asleep")
	workouts, _ := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: store.AddDays(today, -6), To: today})
	met, total := 0, 0
	goals, _ := s.store.ListGoals(ctx, uid)
	for _, g := range goals {
		if !g.Active || g.Period != "day" {
			continue
		}
		total++
		if h, _ := s.store.GoalHits(ctx, uid, g.Metric, today, today); h[today] {
			met++
		}
	}
	meds, _ := s.store.MedsForDay(ctx, uid, today)
	due := 0
	for _, m := range meds {
		for _, sl := range m.Slots {
			if sl.Time != "" && sl.Status == "" {
				due++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"steps": int(get("steps")), "active_kcal": int(get("active_energy")), "exercise_min": int(get("exercise_min")),
		"sleep_min": int(sleepMin), "sleep_text": fmtMin(int(sleepMin)), "resting_hr": int(rhr), "weight": weight, "weight_text": fmtKg(weight),
		"workouts_week": len(workouts), "goals_met": met, "goals_total": total, "goals_text": fmt.Sprintf("%d/%d", met, total), "meds_due": due,
	})
}

func fmtMin(m int) string {
	if m <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

func fmtKg(kg float64) string {
	if kg <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f kg", kg)
}

// handleDay is one person's health day for Pocket Day (and the summary endpoint).
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chi.URLParam(r, "date")
	if !validDate(date) || date == "" {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	vals, err := s.store.DayValues(ctx, uid, date)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	workouts, _ := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: date, To: date})
	nights, _ := s.store.ListSleep(ctx, uid, date, date, 5)
	goals, _ := s.store.ListGoals(ctx, uid)
	var met []string
	for _, g := range goals {
		if h, _ := s.store.GoalHits(ctx, uid, g.Metric, date, date); h[date] {
			met = append(met, g.Metric)
		}
	}
	if met == nil {
		met = []string{}
	}
	numbers := map[string]any{}
	for k, p := range vals {
		m := metrics.Get(k)
		numbers[k] = map[string]any{"value": metrics.Round(m, p.Value), "value2": p.Value2, "unit": m.Unit, "name": m.Name}
	}
	var parts []string
	if p, ok := vals["steps"]; ok {
		parts = append(parts, fmt.Sprintf("%d steps", int(p.Value)))
	}
	if p, ok := vals["sleep_asleep"]; ok {
		parts = append(parts, "slept "+fmtMin(int(p.Value)))
	}
	for _, wk := range workouts {
		parts = append(parts, fmt.Sprintf("%s %d min", strings.ToLower(store.WorkoutKindName(wk.Kind)), wk.DurationS/60))
	}
	if p, ok := vals["resting_hr"]; ok {
		parts = append(parts, fmt.Sprintf("resting HR %d", int(p.Value)))
	}
	if p, ok := vals["weight"]; ok {
		parts = append(parts, fmt.Sprintf("%.1f kg", p.Value))
	}
	// medications and symptoms are private: only in the owner's own UI, never for links
	var meds []store.Medication
	var symptoms []store.Symptom
	if linkApp(ctx) == "" {
		meds, _ = s.store.MedsForDay(ctx, uid, date)
		symptoms, _ = s.store.ListSymptoms(ctx, uid, date, date, 50)
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "metrics": numbers, "workouts": workouts, "sleep": nights, "goals_met": met,
		"summary": strings.Join(parts, " · "), "medications": meds, "symptoms": symptoms})
}

// handleOverlay lists dated items for Pocket Calendar: workouts as timed
// events, nights as all-day notes.
func (s *Server) handleOverlay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	from, to := dateRange(r, 30)
	workouts, _ := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: from, To: to})
	nights, _ := s.store.ListSleep(ctx, uid, from, to, 0)
	type item struct {
		ID       string `json:"id"`
		Ref      string `json:"ref"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle,omitempty"`
		Start    string `json:"start"`
		End      string `json:"end"`
		AllDay   bool   `json:"all_day"`
		URL      string `json:"url,omitempty"`
		Color    string `json:"color,omitempty"`
	}
	out := []item{}
	for _, wk := range workouts {
		sub := fmt.Sprintf("%d min", wk.DurationS/60)
		if wk.DistanceM > 0 {
			sub += fmt.Sprintf(" · %.1f km", wk.DistanceM/1000)
		}
		if wk.EnergyKcal > 0 {
			sub += fmt.Sprintf(" · %d kcal", int(wk.EnergyKcal))
		}
		out = append(out, item{ID: fmt.Sprintf("workout:%d", wk.ID), Ref: fmt.Sprint(wk.ID), Kind: "workout", Title: store.WorkoutKindName(wk.Kind), Subtitle: sub, Start: wk.StartAt, End: wk.EndAt, URL: s.cfg.BaseURL + fmt.Sprintf("/workouts/%d", wk.ID), Color: "#22c55e"})
	}
	for _, n := range nights {
		out = append(out, item{ID: fmt.Sprintf("sleep:%d", n.ID), Ref: fmt.Sprint(n.ID), Kind: "sleep", Title: "Slept " + fmtMin(n.AsleepS/60), Start: n.Date, End: n.Date, AllDay: true, URL: s.cfg.BaseURL + "/sleep", Color: "#6366f1"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": out})
}
