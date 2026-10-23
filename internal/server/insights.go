package server

import (
	"context"
	"math"
	"net/http"
	"sort"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// handleInsights computes the Insights page for a range: trends per metric,
// weekday patterns, workouts by kind, sleep regularity, a few correlations
// and the weather split.
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	days := rangeDays(r.URL.Query().Get("range"))
	to := store.Today()
	from := store.AddDays(to, -(days - 1))
	out, err := s.insights(ctx, uid, from, to)
	if err != nil {
		s.fail(w, err, "insights")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type trend struct {
	Metric  metrics.Metric     `json:"metric"`
	Avg     float64            `json:"avg"`
	PrevAvg float64            `json:"prev_avg"`
	Change  float64            `json:"change_pct"`
	Best    store.DailyPoint   `json:"best"`
	Worst   store.DailyPoint   `json:"worst"`
	Days    int                `json:"days"`
	Weekly  []store.DailyPoint `json:"weekly"`
	PerWeek float64            `json:"per_week"` // for last-value metrics (weight): slope
}

type correlation struct {
	A, B  metrics.Metric `json:"-"`
	AKey  string         `json:"a"`
	BKey  string         `json:"b"`
	AName string         `json:"a_name"`
	BName string         `json:"b_name"`
	R     float64        `json:"r"`
	N     int            `json:"n"`
	Text  string         `json:"text"`
}

func (s *Server) insights(ctx context.Context, uid int64, from, to string) (map[string]any, error) {
	stats, err := s.store.MetricStats(ctx, uid)
	if err != nil {
		return nil, err
	}
	days := daysBetween(from, to) + 1
	prevFrom, prevTo := store.AddDays(from, -days), store.AddDays(from, -1)
	keys := make([]string, 0, len(stats))
	for _, st := range stats {
		keys = append(keys, st.Metric)
	}
	cur, _ := s.store.DailyMany(ctx, uid, keys, from, to)
	prev, _ := s.store.DailyMany(ctx, uid, keys, prevFrom, prevTo)
	var trends []trend
	for _, k := range keys {
		pts := cur[k]
		if len(pts) < 2 {
			continue
		}
		m := metrics.Get(k)
		t := trend{Metric: m, Days: len(pts)}
		vals := make([]float64, 0, len(pts))
		best, worst := pts[0], pts[0]
		for _, p := range pts {
			vals = append(vals, p.Value)
			if p.Value > best.Value {
				best = p
			}
			if p.Value < worst.Value {
				worst = p
			}
		}
		if m.HigherIsBetter != nil && !*m.HigherIsBetter {
			best, worst = worst, best
		}
		t.Avg = metrics.Round(m, mean(vals))
		t.Best, t.Worst = best, worst
		if pp := prev[k]; len(pp) > 0 {
			pv := make([]float64, 0, len(pp))
			for _, p := range pp {
				pv = append(pv, p.Value)
			}
			t.PrevAvg = metrics.Round(m, mean(pv))
			if t.PrevAvg != 0 {
				t.Change = math.Round((t.Avg-t.PrevAvg)/math.Abs(t.PrevAvg)*1000) / 10
			}
		}
		t.Weekly = store.WeeklyAverages(pts, m.Agg == metrics.Sum)
		if m.Agg == metrics.Last {
			t.PerWeek = metrics.Round(m, slopePerDay(pts)*7)
		}
		trends = append(trends, t)
	}
	order := map[metrics.Category]int{}
	for i, c := range metrics.CategoryNames {
		order[c.Key] = i
	}
	sort.SliceStable(trends, func(i, j int) bool {
		if order[trends[i].Metric.Category] != order[trends[j].Metric.Category] {
			return order[trends[i].Metric.Category] < order[trends[j].Metric.Category]
		}
		return trends[i].Days > trends[j].Days
	})
	if trends == nil {
		trends = []trend{}
	}
	// weekday patterns for the headline metrics
	patterns := map[string]any{}
	for _, k := range []string{"steps", "sleep_asleep", "active_energy", "exercise_min", "resting_hr", "hrv"} {
		if pts := cur[k]; len(pts) >= 7 {
			patterns[k] = weekdayPattern(pts, metrics.Get(k))
		}
	}
	// workouts
	byKind, _ := s.store.WorkoutStats(ctx, uid, from, to)
	prevKind, _ := s.store.WorkoutStats(ctx, uid, prevFrom, prevTo)
	wk := map[string]any{"by_kind": byKind, "count": 0, "prev_count": 0, "minutes": 0, "prev_minutes": 0}
	for _, k := range byKind {
		wk["count"] = wk["count"].(int) + k.Count
		wk["minutes"] = wk["minutes"].(int) + k.DurationS/60
	}
	for _, k := range prevKind {
		wk["prev_count"] = wk["prev_count"].(int) + k.Count
		wk["prev_minutes"] = wk["prev_minutes"].(int) + k.DurationS/60
	}
	// sleep regularity: std dev of bedtime and wake time (minutes), in local wall time
	nights, _ := s.store.ListSleep(ctx, uid, from, to, 0)
	sleep := sleepRegularity(nights)
	// correlations between day values
	var cors []correlation
	pairs := [][2]string{{"sleep_asleep", "resting_hr"}, {"sleep_asleep", "hrv"}, {"steps", "sleep_asleep"}, {"exercise_min", "resting_hr"}, {"steps", "resting_hr"}, {"active_energy", "sleep_deep"}, {"alcohol", "resting_hr"}, {"alcohol", "hrv"}, {"caffeine", "sleep_asleep"}, {"sleep_asleep", "mood"}, {"steps", "mood"}, {"sleep_asleep", "energy_level"}, {"mindful_min", "stress"}, {"water", "headache"}}
	for _, p := range pairs {
		a, b := cur[p[0]], cur[p[1]]
		if len(a) < 10 || len(b) < 10 {
			continue
		}
		// resting HR / HRV / mood of the day after a night: shift sleep by 0 (sleep date = wake morning), others by 0
		r, n := pearson(a, b, 0)
		if n < 10 || math.IsNaN(r) {
			continue
		}
		ma, mb := metrics.Get(p[0]), metrics.Get(p[1])
		c := correlation{A: ma, B: mb, AKey: p[0], BKey: p[1], AName: ma.Name, BName: mb.Name, R: math.Round(r*100) / 100, N: n}
		c.Text = describeCorrelation(c)
		if math.Abs(r) >= 0.2 {
			cors = append(cors, c)
		}
	}
	sort.Slice(cors, func(i, j int) bool { return math.Abs(cors[i].R) > math.Abs(cors[j].R) })
	if cors == nil {
		cors = []correlation{}
	}
	// weather split
	weather, _ := s.weatherInsights(ctx, uid, from, to, cur)
	// goal streaks
	goals, _ := s.store.ListGoals(ctx, uid)
	var gs []map[string]any
	for _, g := range goals {
		if !g.Active {
			continue
		}
		hits, _ := s.store.GoalHits(ctx, uid, g.Metric, from, to)
		curS, best, _ := s.store.GoalStreak(ctx, uid, g.Metric)
		gs = append(gs, map[string]any{"metric": g.Metric, "name": metrics.Get(g.Metric).Name, "target": g.Target, "direction": g.Direction, "met": len(hits), "days": days, "pct": len(hits) * 100 / max(1, days), "streak": curS, "best": best})
	}
	if gs == nil {
		gs = []map[string]any{}
	}
	return map[string]any{"from": from, "to": to, "days": days, "trends": trends, "patterns": patterns, "workouts": wk, "sleep": sleep, "correlations": cors, "weather": weather, "goals": gs}, nil
}

func daysBetween(a, b string) int {
	ta, _ := store.ParseDate(a)
	tb, _ := store.ParseDate(b)
	return int(tb.Sub(ta).Hours() / 24)
}

// pearson correlates two daily series on their common dates; shiftB moves
// series B by days (positive = B of the day after).
func pearson(a, b []store.DailyPoint, shiftB int) (float64, int) {
	bm := map[string]float64{}
	for _, p := range b {
		bm[store.AddDays(p.Date, -shiftB)] = p.Value
	}
	var xs, ys []float64
	for _, p := range a {
		if v, ok := bm[p.Date]; ok {
			xs = append(xs, p.Value)
			ys = append(ys, v)
		}
	}
	n := len(xs)
	if n < 3 {
		return math.NaN(), n
	}
	mx, my := mean(xs), mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN(), n
	}
	return sxy / math.Sqrt(sxx*syy), n
}

func describeCorrelation(c correlation) string {
	strength := "a weak"
	switch {
	case math.Abs(c.R) >= 0.6:
		strength = "a strong"
	case math.Abs(c.R) >= 0.4:
		strength = "a moderate"
	}
	dir := "higher"
	if c.R < 0 {
		dir = "lower"
	}
	return "On days with more " + lower(c.AName) + ", " + lower(c.BName) + " tends to be " + dir + " (" + strength + " link, r = " + trimNum(c.R) + ", " + itoa(c.N) + " days)."
}

func lower(s string) string {
	if s == "" {
		return s
	}
	// keep acronyms
	if s == "VO₂ max" || s == "REM sleep" {
		return s
	}
	return string([]rune(s)[0]+32) + s[1:]
}

func itoa(n int) string { return trimNum(float64(n)) }

// sleepRegularity summarises bed and wake times.
func sleepRegularity(nights []store.SleepSession) map[string]any {
	if len(nights) == 0 {
		return map[string]any{"nights": 0}
	}
	var beds, wakes, asleep []float64
	for _, n := range nights {
		bt, err1 := parseRFC(n.StartAt)
		wt, err2 := parseRFC(n.EndAt)
		if err1 != nil || err2 != nil {
			continue
		}
		bm := float64(bt.Hour()*60 + bt.Minute())
		if bm < 12*60 {
			bm += 24 * 60 // after midnight counts as late
		}
		beds = append(beds, bm)
		wakes = append(wakes, float64(wt.Hour()*60+wt.Minute()))
		asleep = append(asleep, float64(n.AsleepS)/60)
	}
	if len(beds) == 0 {
		return map[string]any{"nights": 0}
	}
	bedAvg, wakeAvg := mean(beds), mean(wakes)
	return map[string]any{"nights": len(beds), "bed_avg": clock(bedAvg), "wake_avg": clock(wakeAvg), "bed_sd_min": math.Round(stddev(beds)), "wake_sd_min": math.Round(stddev(wakes)),
		"asleep_avg_min": math.Round(mean(asleep)), "asleep_sd_min": math.Round(stddev(asleep)), "score": regularityScore(stddev(beds), stddev(wakes))}
}

// regularityScore: 100 when bed and wake times never move, falling with the spread.
func regularityScore(bedSD, wakeSD float64) int {
	sd := (bedSD + wakeSD) / 2
	return int(math.Max(0, math.Min(100, 100-sd*0.8)))
}

func clock(min float64) string {
	m := int(math.Round(min)) % (24 * 60)
	return itoa2(m/60) + ":" + itoa2(m%60)
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func stddev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)-1))
}

// weatherInsights splits the headline metrics by the day's weather.
func (s *Server) weatherInsights(ctx context.Context, uid int64, from, to string, cur map[string][]store.DailyPoint) (map[string]any, error) {
	weather, err := s.store.Weather(ctx, uid, from, to)
	if err != nil || len(weather) == 0 {
		return nil, err
	}
	keys := []string{"steps", "exercise_min", "active_energy", "sleep_asleep", "mood"}
	type acc struct {
		days int
		sum  map[string]float64
		n    map[string]int
	}
	newAcc := func() *acc { return &acc{sum: map[string]float64{}, n: map[string]int{}} }
	byDate := map[string]map[string]float64{}
	for _, k := range keys {
		for _, p := range cur[k] {
			if byDate[p.Date] == nil {
				byDate[p.Date] = map[string]float64{}
			}
			byDate[p.Date][k] = p.Value
		}
	}
	byCond := map[string]*acc{}
	tempBands := []struct {
		key, label string
		lo, hi     float64
	}{{"cold", "Below 5 °C", -1e9, 5}, {"cool", "5–15 °C", 5, 15}, {"mild", "15–25 °C", 15, 25}, {"hot", "Above 25 °C", 25, 1e9}}
	byTemp := map[string]*acc{}
	rain := map[string]*acc{"dry": newAcc(), "wet": newAcc()}
	add := func(a *acc, date string) {
		a.days++
		for k, v := range byDate[date] {
			a.sum[k] += v
			a.n[k]++
		}
	}
	for date, wd := range weather {
		if wd.Condition != "" {
			if byCond[wd.Condition] == nil {
				byCond[wd.Condition] = newAcc()
			}
			add(byCond[wd.Condition], date)
		}
		if wd.TMax != nil {
			for _, b := range tempBands {
				if *wd.TMax >= b.lo && *wd.TMax < b.hi {
					if byTemp[b.key] == nil {
						byTemp[b.key] = newAcc()
					}
					add(byTemp[b.key], date)
				}
			}
		}
		if wd.PrecipMM != nil {
			if *wd.PrecipMM >= 1 {
				add(rain["wet"], date)
			} else {
				add(rain["dry"], date)
			}
		}
	}
	finish := func(key, label string, a *acc) map[string]any {
		row := map[string]any{"key": key, "label": label, "days": a.days}
		for _, k := range keys {
			if a.n[k] > 0 {
				row[k] = metrics.Round(metrics.Get(k), a.sum[k]/float64(a.n[k]))
			}
		}
		return row
	}
	var conds, temps, rains []map[string]any
	for _, c := range store.Conditions {
		if a := byCond[c]; a != nil {
			conds = append(conds, finish(c, store.ConditionLabel(c), a))
		}
	}
	for _, b := range tempBands {
		if a := byTemp[b.key]; a != nil {
			temps = append(temps, finish(b.key, b.label, a))
		}
	}
	if rain["dry"].days > 0 {
		rains = append(rains, finish("dry", "Dry days", rain["dry"]))
	}
	if rain["wet"].days > 0 {
		rains = append(rains, finish("wet", "Rainy days", rain["wet"]))
	}
	return map[string]any{"days": len(weather), "by_condition": conds, "by_temperature": temps, "by_rain": rains, "metrics": keys}, nil
}
