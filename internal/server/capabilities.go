package server

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Health (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. They read what the Today page, /day and the lists read, as the
// request's person, and change nothing. Medications, symptoms and cycle
// tracking never leave through them, not even to the person's own session:
// they are for the app's own pages only. Numbers are in the person's units,
// as the app shows them.

// hasWord reports whether there is a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// dots joins the parts of a line that are there: "35 min · 5.20 km · 310 kcal".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// numText is a number as the pages show it: thousands grouped, at most dec
// decimals, no trailing zeros ("8,240", "82.4").
func numText(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		return sign + b.String() + "." + frac
	}
	return sign + b.String()
}

// minutesText is a length of time as the pages show it: "7h 20m", "45 min".
func minutesText(min float64) string {
	m := int(math.Round(min))
	if m <= 0 {
		return ""
	}
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", m)
}

// valueText is a day's number of a metric as the pages show it, in the
// person's units: "8,240", "82.4 kg", "7h 20m", "121/79 mmHg".
func valueText(m metrics.Metric, v float64, v2 *float64, imperial bool) string {
	if strings.HasPrefix(m.Key, "sleep_") || (m.Unit == "min" && m.Agg == metrics.Sum && v >= 90) {
		return minutesText(v)
	}
	if m.Pair && v2 != nil {
		return fmt.Sprintf("%d/%d %s", int(math.Round(v)), int(math.Round(*v2)), m.Unit)
	}
	d, u := metrics.Display(m, v, imperial)
	s := numText(d, m.Decimals)
	switch u {
	case "", "count", "1–5", "drinks":
		return s
	}
	return s + " " + u
}

// amountText is valueText with the thing counted named when the number has
// no unit: "8,000 steps", "12 stand hours", "80 kg".
func amountText(m metrics.Metric, v float64, imperial bool) string {
	s := valueText(m, v, nil, imperial)
	if _, u := metrics.Display(m, v, imperial); u == "count" && !strings.HasPrefix(m.Key, "sleep_") {
		s += " " + strings.ToLower(m.Name)
	}
	return s
}

// goalText is a goal in words: "At least 8,000 steps a day", "At most 80 kg".
func goalText(g store.Goal, imperial bool) string {
	m := metrics.Get(g.Metric)
	s := "At least "
	if g.Direction == "at_most" {
		s = "At most "
	}
	s += amountText(m, g.Target, imperial)
	switch g.Period {
	case "day":
		s += " a day"
	case "week":
		s += " a week"
	}
	return s
}

// durationText is a workout's length as the lists show it: "1h 05m", "35 min".
func durationText(secs int) string {
	if secs <= 0 {
		return ""
	}
	h, m := secs/3600, (secs%3600)/60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%d min", m)
}

// distanceText is a distance as the lists show it: "850 m", "5.20 km", "12.4 km", "3.23 mi".
func distanceText(meters float64, imperial bool) string {
	switch {
	case meters <= 0:
		return ""
	case imperial && meters < 16093:
		return fmt.Sprintf("%.2f mi", meters/1609.344)
	case imperial:
		return fmt.Sprintf("%.1f mi", meters/1609.344)
	case meters < 1000:
		return fmt.Sprintf("%d m", int(math.Round(meters)))
	case meters < 10000:
		return fmt.Sprintf("%.2f km", meters/1000)
	}
	return fmt.Sprintf("%.1f km", meters/1000)
}

// workoutTitle is a workout's line, as the lists show it: its name, else its kind.
func workoutTitle(w store.Workout) string {
	if n := strings.TrimSpace(w.Name); n != "" {
		return n
	}
	return kindName(w.Kind)
}

// kindName is a workout kind as people read it ("Strength training").
func kindName(kind string) string {
	if kind == "" {
		return "Workout"
	}
	return store.WorkoutKindName(kind)
}

// workoutNumbers are a workout's headline numbers: "35 min · 5.20 km · 310 kcal".
func workoutNumbers(w store.Workout, imperial bool) string {
	kcal := ""
	if w.EnergyKcal >= 1 {
		kcal = numText(math.Round(w.EnergyKcal), 0) + " kcal"
	}
	return dots(durationText(w.DurationS), distanceText(w.DistanceM, imperial), kcal)
}

// sameKind is the workout's kind when its title does not say it already.
func sameKind(w store.Workout) string {
	k := kindName(w.Kind)
	if strings.Contains(strings.ToLower(workoutTitle(w)), strings.ToLower(k)) {
		return ""
	}
	return k
}

// moment reads a stored time ("2026-11-27T07:00:00Z").
func moment(s string) (time.Time, bool) {
	for _, f := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// timeOfDay is a time of day the way the person reads it (Settings → time format).
func timeOfDay(t time.Time, format string) string {
	if format == "12h" {
		return t.Format("3:04 PM")
	}
	return t.Format("15:04")
}

// dateText is a day in words: "today", "yesterday", "24 Nov 2026".
func dateText(date string) string {
	switch date {
	case store.Today():
		return "today"
	case store.AddDays(store.Today(), -1):
		return "yesterday"
	}
	if t, err := store.ParseDate(date); err == nil {
		return t.Format("2 Jan 2006")
	}
	return date
}

// whenText is when a workout started: "today, 07:00", "24 Nov 2026, 18:30".
func whenText(w store.Workout, st store.Settings) string {
	when := dateText(w.Date)
	if t, ok := moment(w.StartAt); ok {
		when += ", " + timeOfDay(t.In(time.Local), st.TimeFormat)
	}
	return when
}

// excerpt is a short piece of a longer text around the words looked for.
func excerpt(text, q string) string {
	const width = 140
	text = strings.Join(strings.Fields(text), " ")
	rs := []rune(text)
	if len(rs) <= width {
		return text
	}
	from := 0
	low := strings.ToLower(text)
	if i := strings.Index(low, strings.ToLower(q)); i > 0 {
		from = utf8.RuneCountInString(low[:i]) - 40
	}
	from = max(0, min(from, len(rs)-width))
	out := strings.TrimSpace(string(rs[from : from+width]))
	if from > 0 {
		out = "…" + out
	}
	if from+width < len(rs) {
		out += "…"
	}
	return out
}

// shareable drops what never leaves the app's own pages (cycle tracking).
func shareable(vals map[string]store.DailyPoint) map[string]store.DailyPoint {
	for k := range vals {
		if metrics.Private(k) {
			delete(vals, k)
		}
	}
	return vals
}

// headline are the day's main numbers in the order the Today page shows its
// tiles (Settings → Home), those with a value that day, at most n, leaving
// out the keys in skip.
func headline(st store.Settings, vals map[string]store.DailyPoint, n int, skip func(key string) bool) []core.Metric {
	var out []core.Metric
	for _, k := range st.HomeMetrics {
		p, ok := vals[k]
		if !ok || metrics.Private(k) || (skip != nil && skip(k)) {
			continue
		}
		m := metrics.Get(k)
		if v := valueText(m, p.Value, p.Value2, st.Units == "imperial"); v != "" {
			out = append(out, core.Metric{Label: m.Name, Value: v})
		}
		if len(out) == n {
			break
		}
	}
	return out
}

// ---------- search ----------

// goalWords find every goal.
var goalWords = map[string]bool{"goal": true, "goals": true, "target": true, "targets": true}

// pocketSearch answers Pocket Search with what has a name a person would
// type: a measurement by its name (one item with its latest value, opening
// its page), goals by their measurement's name ("goals" lists them all),
// the planned workouts by their name and workouts by kind, name, place or
// notes, newest first. Daily numbers are not searched one by one.
// Medications, symptoms and cycle tracking are never searched. Words
// without a letter or a digit find nothing.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	imperial := st.Units == "imperial"
	lq := strings.ToLower(q)
	has := func(v ...string) bool { return strings.Contains(strings.ToLower(strings.Join(v, " ")), lq) }
	out := []core.SearchItem{}

	// a measurement by its name, with its latest value
	found := 0
	for _, m := range metrics.Sorted() {
		if found == 5 {
			break
		}
		if metrics.Private(m.Key) || !has(m.Name) {
			continue
		}
		p, err := s.store.LatestDaily(ctx, uid, m.Key)
		if err != nil {
			continue
		}
		found++
		score := 3.0
		if strings.HasPrefix(strings.ToLower(m.Name), lq) {
			score = 4
		}
		out = append(out, core.SearchItem{ID: "measurement:" + m.Key, Title: m.Name, Kind: "measurement", Date: p.Date, URL: "/metrics/" + m.Key, Score: score,
			Snippet: dots("Latest "+valueText(m, p.Value, p.Value2, imperial), dateText(p.Date))})
	}

	// goals
	goals, err := s.store.ListGoals(ctx, uid)
	if err != nil {
		return nil, err
	}
	var vals map[string]store.DailyPoint
	for _, g := range goals {
		m := metrics.Get(g.Metric)
		if metrics.Private(g.Metric) || !(goalWords[lq] || has(m.Name)) {
			continue
		}
		if vals == nil {
			vals, _ = s.store.DayValues(ctx, uid, store.Today())
		}
		state := ""
		if !g.Active {
			state = "turned off"
		} else if h, _ := s.store.GoalHits(ctx, uid, g.Metric, store.Today(), store.Today()); h[store.Today()] {
			state = "met today"
		} else if p, ok := vals[g.Metric]; ok {
			state = valueText(m, p.Value, p.Value2, imperial) + " today"
		}
		out = append(out, core.SearchItem{ID: "goal:" + strconv.FormatInt(g.ID, 10), Title: m.Name + " goal", Kind: "goal", URL: "/goals", Score: 3,
			Snippet: dots(goalText(g, imperial), state)})
	}

	// planned workouts
	plans, err := s.store.SearchPlans(ctx, uid, q, limit)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		it := core.SearchItem{ID: "plan:" + p.ID, Title: p.Name, Kind: "plan", Date: p.Last, URL: "/workouts", Score: 3}
		if p.ID != "" {
			it.URL = "/workouts?plan=" + url.QueryEscape(p.ID)
		} else {
			it.ID = "plan:" + p.Name
		}
		sessions := "1 session"
		if p.Sessions != 1 {
			sessions = fmt.Sprintf("%d sessions", p.Sessions)
		}
		it.Snippet = dots("Planned workout", sessions, "last "+dateText(p.Last))
		out = append(out, it)
	}

	// workouts by kind, name, place or notes
	var kinds []string
	for _, k := range store.WorkoutKinds {
		if has(k.Name) {
			kinds = append(kinds, k.Key)
		}
	}
	workouts, err := s.store.SearchWorkouts(ctx, uid, q, kinds, limit)
	if err != nil {
		return nil, err
	}
	for _, w := range workouts {
		it := core.SearchItem{ID: "workout:" + strconv.FormatInt(w.ID, 10), Title: workoutTitle(w), Kind: "workout", Date: w.Date,
			URL: fmt.Sprintf("/workouts/%d", w.ID), Score: 1,
			Snippet: dots(whenText(w, st), sameKind(w), workoutNumbers(w, imperial), w.Place)}
		if has(it.Title, kindName(w.Kind)) {
			it.Score = 2
		}
		if !has(it.Title, it.Snippet, kindName(w.Kind), w.Kind, w.PlanName) && has(w.Notes) {
			it.Snippet = dots(it.Snippet, "Notes: "+excerpt(w.Notes, q))
		}
		out = append(out, it)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// clockOn is the time of day of a stored moment when it falls on that day in
// the zone, else "".
func clockOn(at, date string, loc *time.Location) string {
	t, ok := moment(at)
	if !ok {
		return ""
	}
	if t = t.In(loc); t.Format("2006-01-02") != date {
		return ""
	}
	return t.Format("15:04")
}

// pocketDay answers Pocket Day with what /day reads for that day: the
// workouts, the night's sleep and the goals met as items, the numbers the
// Today page leads with (its first tiles, sleep left out as it is an item) as
// metrics, and the day's line as /day writes it. A day's date is the one the
// app gave it when the data came in; the zone only places the workouts on
// the clock. Medications, symptoms and cycle tracking are left out.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	vals, err := s.store.DayValues(ctx, uid, date)
	if err != nil {
		return nil, err
	}
	vals = shareable(vals)
	workouts, err := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: date, To: date})
	if err != nil {
		return nil, err
	}
	nights, err := s.store.ListSleep(ctx, uid, date, date, 5)
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 && len(workouts) == 0 && len(nights) == 0 {
		return nil, nil
	}
	if tz == nil {
		tz = time.Local
	}
	st, _ := s.store.GetSettings(ctx, uid)
	imperial := st.Units == "imperial"
	day := &core.Day{Date: date, Summary: daySummary(vals, workouts, imperial)}
	// oldest first, as the day went
	for i := len(workouts) - 1; i >= 0; i-- {
		w := workouts[i]
		it := core.DayItem{ID: "workout:" + strconv.FormatInt(w.ID, 10), Title: workoutTitle(w), Kind: "workout", URL: fmt.Sprintf("/workouts/%d", w.ID),
			Sub: dots(sameKind(w), workoutNumbers(w, imperial), w.Place), Minutes: math.Round(float64(w.DurationS) / 60), Time: clockOn(w.StartAt, date, tz)}
		if w.DistanceM > 0 {
			it.Qty = math.Round(w.DistanceM/100) / 10
		}
		if it.Time != "" {
			if end := clockOn(w.EndAt, date, tz); end > it.Time {
				it.End = end
			}
		}
		day.Items = append(day.Items, it)
	}
	for _, n := range nights {
		it := core.DayItem{ID: "sleep:" + strconv.FormatInt(n.ID, 10), Kind: "sleep", URL: "/sleep"}
		switch {
		// whole minutes, as the day's line and Pocket Calendar's overlay count them
		case n.AsleepS >= 60:
			it.Title = "Slept " + minutesText(float64(n.AsleepS/60))
		case n.InBedS >= 60:
			it.Title = "In bed " + minutesText(float64(n.InBedS/60))
		default:
			continue
		}
		from, ok1 := moment(n.StartAt)
		to, ok2 := moment(n.EndAt)
		if ok1 && ok2 {
			it.Sub = timeOfDay(from.In(tz), st.TimeFormat) + " to " + timeOfDay(to.In(tz), st.TimeFormat)
		}
		day.Items = append(day.Items, it)
	}
	goals, err := s.store.ListGoals(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, g := range goals {
		if metrics.Private(g.Metric) {
			continue
		}
		if h, _ := s.store.GoalHits(ctx, uid, g.Metric, date, date); h[date] {
			day.Items = append(day.Items, core.DayItem{ID: "goal:" + strconv.FormatInt(g.ID, 10), Title: metrics.Get(g.Metric).Name + " goal met",
				Sub: goalText(g, imperial), Kind: "goal", Done: true, URL: "/goals"})
		}
	}
	day.Metrics = headline(st, vals, 4, func(k string) bool { return strings.HasPrefix(k, "sleep_") })
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub with today so far, from what is stored (no
// phone or other app is asked): the steps against the steps goal as the
// line, last night's sleep and the Today page's next tiles as numbers, and
// today's workouts and daily goals as rows. Before anything came in today
// there is nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	today := store.Today()
	vals, err := s.store.DayValues(ctx, uid, today)
	if err != nil {
		return nil, err
	}
	vals = shareable(vals)
	workouts, err := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: today, To: today, Limit: 8})
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 && len(workouts) == 0 {
		return nil, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	imperial := st.Units == "imperial"
	goals, err := s.store.ListGoals(ctx, uid)
	if err != nil {
		return nil, err
	}
	w := &core.WidgetData{URL: "/"}

	steps, hasSteps := vals["steps"]
	var stepGoal *store.Goal
	for i, g := range goals {
		if g.Metric == "steps" && g.Active && g.Period == "day" && g.Direction != "at_most" && g.Target > 0 {
			stepGoal = &goals[i]
		}
	}
	sleep, hasSleep := vals["sleep_asleep"]
	switch {
	case hasSteps && stepGoal != nil:
		w.Line = numText(math.Round(steps.Value), 0) + " of " + numText(stepGoal.Target, 0) + " steps"
	case hasSteps:
		w.Line = numText(math.Round(steps.Value), 0) + " steps"
	case hasSleep && sleep.Value > 0:
		w.Line = "Slept " + minutesText(sleep.Value)
	case len(workouts) > 0:
		w.Line = dots(workoutTitle(workouts[0]), durationText(workouts[0].DurationS))
	}

	if hasSleep && sleep.Value > 0 && hasSteps {
		w.Metrics = append(w.Metrics, core.Metric{Label: "Sleep", Value: minutesText(sleep.Value)})
	}
	w.Metrics = append(w.Metrics, headline(st, vals, 4-len(w.Metrics), func(k string) bool { return k == "steps" || strings.HasPrefix(k, "sleep_") })...)

	// today's workouts, the latest first
	for _, wk := range workouts {
		when := ""
		if t, ok := moment(wk.StartAt); ok {
			when = timeOfDay(t.In(time.Local), st.TimeFormat)
		}
		w.Items = append(w.Items, core.WidgetItem{ID: "workout:" + strconv.FormatInt(wk.ID, 10), Title: workoutTitle(wk),
			Detail: dots(when, workoutNumbers(wk, imperial)), Done: true, URL: fmt.Sprintf("/workouts/%d", wk.ID)})
	}
	// the day's goals that have a number today
	for _, g := range goals {
		if !g.Active || g.Period != "day" || metrics.Private(g.Metric) {
			continue
		}
		p, ok := vals[g.Metric]
		if !ok {
			continue
		}
		m := metrics.Get(g.Metric)
		h, _ := s.store.GoalHits(ctx, uid, g.Metric, today, today)
		detail := valueText(m, p.Value, p.Value2, imperial) + " of " + valueText(m, g.Target, nil, imperial)
		if g.Direction == "at_most" {
			detail = valueText(m, p.Value, p.Value2, imperial) + ", at most " + valueText(m, g.Target, nil, imperial)
		}
		w.Items = append(w.Items, core.WidgetItem{ID: "goal:" + strconv.FormatInt(g.ID, 10), Title: m.Name + " goal", Detail: detail, Done: h[today], URL: "/goals"})
	}
	return w, nil
}
