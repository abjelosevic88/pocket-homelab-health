package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- what was recorded along a workout ----
//
// The phone sends a workout's series (heart rate, speed, power, cadence,
// distance… every few seconds), its laps, segments and planned intervals, and
// its route. GET /workouts/{id}/track returns them with each part worked out
// (time, distance, pace, heart rate, power, cadence, climb) and splits per
// kilometre or mile, so the page and any other reader get the same numbers.

type partView struct {
	store.WorkoutSegment
	N          int     `json:"n,omitempty"`
	DurationS  float64 `json:"duration_s"`
	DistanceM  float64 `json:"distance_m"`
	PaceSKm    float64 `json:"pace_s_km,omitempty"`
	SpeedMS    float64 `json:"speed_ms,omitempty"`
	HRAvg      float64 `json:"hr_avg,omitempty"`
	HRMax      float64 `json:"hr_max,omitempty"`
	PowerAvg   float64 `json:"power_avg,omitempty"`
	CadenceAvg float64 `json:"cadence_avg,omitempty"`
	ClimbM     float64 `json:"climb_m"`
	// The step's target (planned workouts) next to what was done.
	Target *targetView `json:"target,omitempty"`
}

// targetView is a planned step's target with what was done: the step's
// average in the target's units (speed in m/s), and the share of the step's
// time below, inside and above the range, from the series.
type targetView struct {
	store.StepTarget
	// min/max in effect: a zone turned into its range (bpm, W) when the zones are known
	RangeMin *float64 `json:"range_min,omitempty"`
	RangeMax *float64 `json:"range_max,omitempty"`
	// "settings" or "estimated" (heart rate zones worked out from max and resting heart rate)
	ZonesFrom string  `json:"zones_from,omitempty"`
	Actual    float64 `json:"actual"`
	// in | below | above (the average against the range); "" when it cannot be told
	Status   string  `json:"status"`
	InPct    float64 `json:"in_pct"`
	BelowPct float64 `json:"below_pct"`
	AbovePct float64 `json:"above_pct"`
	// seconds of the step the series covered
	CoveredS float64 `json:"covered_s"`
}

// zones are the person's heart rate and power zones: the top of zones 1…n-1.
type zones struct {
	HR, Power []float64
	HRFrom    string
}

type trackView struct {
	Route    [][4]float64 `json:"route"`
	Series   store.Series `json:"series"`
	Segments []partView   `json:"segments"`
	Splits   []partView   `json:"splits"`
	// "km" or "mi"
	SplitUnit string `json:"split_unit"`
	// where distances come from: "series" (the device's own distance), "route" or ""
	DistanceFrom string `json:"distance_from"`
	// The spacing of the series returned, in seconds; 0 = the device's own rate.
	SeriesStep float64 `json:"series_step"`
	// The spacing they were recorded (sent) at; 0 = the device's own rate.
	RecordedStep float64 `json:"recorded_step"`
	UpdatedAt    string  `json:"updated_at,omitempty"`
}

func (s *Server) handleWorkoutTrack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wk, err := s.store.GetWorkout(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	t, err := s.store.WorkoutTrack(ctx, wk.ID)
	if err != nil {
		s.fail(w, err, "track")
		return
	}
	unit := r.URL.Query().Get("unit")
	if unit != "km" && unit != "mi" {
		unit = "km"
		if st, err := s.store.GetSettings(ctx, uid); err == nil && st.Units == "imperial" {
			unit = "mi"
		}
	}
	// ?step=5 averages the series into 5-second buckets (the older shape);
	// left out (or step=native/0) they come as recorded.
	step := 0.0
	if v := r.URL.Query().Get("step"); v != "" && v != "native" {
		if _, err := fmt.Sscanf(v, "%g", &step); err != nil || step < 0 || step > 3600 {
			writeErr(w, http.StatusBadRequest, "step is seconds (1–3600) or native")
			return
		}
	}
	tv := buildTrack(t, unit, step, s.zonesFor(ctx, uid))
	// another app reading through a link gets the numbers, not where you were
	if linkApp(ctx) != "" {
		tv.Route = [][4]float64{}
	}
	writeJSON(w, http.StatusOK, tv)
}

func buildTrack(t *store.WorkoutTrack, unit string, step float64, z zones) trackView {
	tv := trackView{Route: [][4]float64{}, Series: store.Series{}, Segments: []partView{}, Splits: []partView{}, SplitUnit: unit}
	if t == nil {
		return tv
	}
	tv.Route, tv.Series, tv.UpdatedAt = t.Route, t.Series, t.UpdatedAt
	tv.SeriesStep, tv.RecordedStep = t.SeriesStep, t.SeriesStep
	// the parts are always worked out from the series as recorded
	a := newAlong(t)
	tv.DistanceFrom = a.from
	if step > 0 && step != t.SeriesStep {
		tv.Series, tv.SeriesStep = resample(t.Series, step), step
	}
	segs := append([]store.WorkoutSegment(nil), t.Segments...)
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].StartS < segs[j].StartS })
	for _, sg := range segs {
		p := a.part(sg, sg.StartS, sg.EndS)
		if sg.Target != nil {
			p.Target = a.target(*sg.Target, p, z)
		}
		tv.Segments = append(tv.Segments, p)
	}
	tv.Segments = dropAutoSplits(tv.Segments)
	size := 1000.0
	if unit == "mi" {
		size = 1609.344
	}
	tv.Splits = a.splits(size)
	return tv
}

// dropAutoSplits leaves out the segments a watch records on its own at every
// kilometre and every mile (two chains running from the start side by side):
// they are the splits again, which have their own table. A chain counts when it
// starts at the start, has at least two pieces, and every piece but the last is
// one kilometre or one mile (within 3%). Segments the person marked stay.
func dropAutoSplits(parts []partView) []partView {
	type chain struct{ idx []int }
	var chains []*chain
	for i, p := range parts {
		if p.Kind != "segment" {
			continue
		}
		var into *chain
		for _, c := range chains {
			if last := parts[c.idx[len(c.idx)-1]]; math.Abs(last.EndS-p.StartS) <= 1 {
				into = c
				break
			}
		}
		if into == nil {
			into = &chain{}
			chains = append(chains, into)
		}
		into.idx = append(into.idx, i)
	}
	drop := map[int]bool{}
	for _, c := range chains {
		if len(c.idx) < 2 || parts[c.idx[0]].StartS > 1 {
			continue
		}
		for _, size := range []float64{1000, 1609.344} {
			ok := true
			for k, i := range c.idx {
				d := parts[i].DistanceM
				if k < len(c.idx)-1 && math.Abs(d-size) > size*0.03 || d > size*1.03 {
					ok = false
					break
				}
			}
			if ok {
				for _, i := range c.idx {
					drop[i] = true
				}
				break
			}
		}
	}
	if len(drop) == 0 {
		return parts
	}
	out := []partView{}
	n := 0
	for i, p := range parts {
		if drop[i] {
			continue
		}
		if p.Kind == "segment" {
			n++
			if strings.HasPrefix(p.Label, "Segment ") {
				p.Label = fmt.Sprintf("Segment %d", n)
			}
		}
		out = append(out, p)
	}
	return out
}

// along answers "how far by then" and "what was recorded between then and then".
type along struct {
	t      *store.WorkoutTrack
	ts, ds []float64 // a running distance over time, from the device's series or the route
	from   string
	cache  map[string][2]any
}

func newAlong(t *store.WorkoutTrack) *along {
	a := &along{t: t}
	if d := t.Series["distance"]; len(d) > 1 {
		a.from = "series"
		for _, p := range d {
			a.ts, a.ds = append(a.ts, p[0]), append(a.ds, p[1])
		}
	} else if len(t.Route) > 1 && t.Route[len(t.Route)-1][3] > 0 {
		a.from = "route"
		total := 0.0
		for i, p := range t.Route {
			if i > 0 {
				total += haversine(t.Route[i-1][0], t.Route[i-1][1], p[0], p[1])
			}
			a.ts, a.ds = append(a.ts, p[3]), append(a.ds, total)
		}
	}
	return a
}

// distAt is the distance covered at a moment (interpolated).
func (a *along) distAt(t float64) float64 {
	return interp(a.ts, a.ds, t)
}

// timeAt is the moment a distance was reached (interpolated).
func (a *along) timeAt(d float64) float64 {
	return interp(a.ds, a.ts, d)
}

// interp reads y at x from points sorted by x, held flat past either end.
func interp(xs, ys []float64, x float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	if x <= xs[0] {
		return ys[0]
	}
	if x >= xs[n-1] {
		return ys[n-1]
	}
	i := sort.SearchFloat64s(xs, x)
	if xs[i] == x || xs[i] == xs[i-1] {
		return ys[i]
	}
	f := (x - xs[i-1]) / (xs[i] - xs[i-1])
	return ys[i-1] + f*(ys[i]-ys[i-1])
}

func (a *along) part(sg store.WorkoutSegment, from, to float64) partView {
	p := partView{WorkoutSegment: sg, DurationS: math.Round(to - from)}
	if a.from != "" {
		p.DistanceM = math.Round(a.distAt(to) - a.distAt(from))
	}
	if p.DistanceM > 0 && p.DurationS > 0 {
		p.SpeedMS = round2(p.DistanceM / p.DurationS)
		p.PaceSKm = math.Round(p.DurationS / (p.DistanceM / 1000))
	} else if v, _ := a.stat("speed", from, to); v > 0 {
		p.SpeedMS = round2(v)
		p.PaceSKm = math.Round(1000 / v)
	}
	p.HRAvg, p.HRMax = a.stat("heart_rate", from, to)
	p.HRAvg, p.HRMax = math.Round(p.HRAvg), math.Round(p.HRMax)
	pw, _ := a.stat("power", from, to)
	cd, _ := a.stat("cadence", from, to)
	p.PowerAvg, p.CadenceAvg = math.Round(pw), math.Round(cd)
	var sub [][4]float64
	for _, q := range a.t.Route {
		if q[3] >= from && q[3] <= to {
			sub = append(sub, q)
		}
	}
	p.ClimbM = store.Ascent(sub)
	return p
}

// cover is how long each reading of a series stands for: until the next
// reading, but not past one and a half times the usual spacing (a gap in the
// recording stays a gap).
func cover(pts [][2]float64) []float64 {
	n := len(pts)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	gaps := make([]float64, 0, n)
	for i := 1; i < n; i++ {
		if d := pts[i][0] - pts[i-1][0]; d > 0 {
			gaps = append(gaps, d)
		}
	}
	limit := 5.0
	if len(gaps) > 0 {
		sort.Float64s(gaps)
		limit = math.Max(1, gaps[len(gaps)/2]*1.5)
	}
	for i := range pts {
		end := pts[i][0] + limit
		if i+1 < n && pts[i+1][0] < end {
			end = pts[i+1][0]
		}
		out[i] = end
	}
	return out
}

// series reads a series with how long each reading stands for. A speed asked
// for with none recorded comes from the distance.
func (a *along) series(key string) ([][2]float64, []float64) {
	if a.cache == nil {
		a.cache = map[string][2]any{}
	}
	if c, ok := a.cache[key]; ok {
		return c[0].([][2]float64), c[1].([]float64)
	}
	pts := a.t.Series[key]
	if len(pts) == 0 && key == "speed" {
		if d := a.t.Series["distance"]; len(d) > 1 {
			for i := 0; i+1 < len(d); i++ {
				if dt := d[i+1][0] - d[i][0]; dt > 0 {
					pts = append(pts, [2]float64{d[i][0], math.Max(0, (d[i+1][1]-d[i][1])/dt)})
				}
			}
		}
	}
	cv := cover(pts)
	a.cache[key] = [2]any{pts, cv}
	return pts, cv
}

// stat is the average (over time) and the highest of a series between two
// moments; a part shorter than the spacing of the series takes the nearest reading.
func (a *along) stat(key string, from, to float64) (avg, max float64) {
	pts, cv := a.series(key)
	if len(pts) == 0 {
		return 0, 0
	}
	sum, wt := 0.0, 0.0
	for i, p := range pts {
		if p[0] >= to {
			break
		}
		if w := math.Min(cv[i], to) - math.Max(p[0], from); w > 0 {
			sum += p[1] * w
			wt += w
			max = math.Max(max, p[1])
		}
	}
	if wt > 0 {
		return sum / wt, max
	}
	mid, best := (from+to)/2, -1
	for i, p := range pts {
		if best < 0 || math.Abs(p[0]-mid) < math.Abs(pts[best][0]-mid) {
			best = i
		}
	}
	if math.Abs(pts[best][0]-mid) > math.Max(15, (to-from)/2+10) {
		return 0, 0
	}
	return pts[best][1], pts[best][1]
}

// targetSeries is the series a target is measured on.
var targetSeries = map[string]string{"heart_rate": "heart_rate", "heart_rate_zone": "heart_rate", "speed": "speed", "cadence": "cadence", "power": "power", "power_zone": "power"}

// zoneRange is zone n (1-based) of zones whose tops are given.
func zoneRange(tops []float64, n int) (lo, hi *float64) {
	if len(tops) == 0 || n < 1 || n > len(tops)+1 {
		return nil, nil
	}
	if n > 1 {
		v := tops[n-2]
		lo = &v
	}
	if n <= len(tops) {
		v := tops[n-1]
		hi = &v
	}
	return lo, hi
}

// target sets a step's target beside what was done in it.
func (a *along) target(t store.StepTarget, p partView, z zones) *targetView {
	tv := &targetView{StepTarget: t, RangeMin: t.Min, RangeMax: t.Max}
	switch t.Type {
	case "heart_rate_zone":
		tv.RangeMin, tv.RangeMax = zoneRange(z.HR, t.Zone)
		if tv.RangeMin != nil || tv.RangeMax != nil {
			tv.ZonesFrom = z.HRFrom
		}
	case "power_zone":
		tv.RangeMin, tv.RangeMax = zoneRange(z.Power, t.Zone)
		if tv.RangeMin != nil || tv.RangeMax != nil {
			tv.ZonesFrom = "settings"
		}
	}
	key := targetSeries[t.Type]
	switch key {
	case "heart_rate":
		tv.Actual = p.HRAvg
	case "speed":
		tv.Actual = p.SpeedMS
	case "cadence":
		tv.Actual = p.CadenceAvg
	case "power":
		tv.Actual = p.PowerAvg
	}
	if tv.Actual == 0 {
		tv.Actual, _ = a.stat(key, p.StartS, p.EndS)
		if key == "speed" {
			tv.Actual = round2(tv.Actual)
		} else {
			tv.Actual = math.Round(tv.Actual)
		}
	}
	if tv.RangeMin == nil && tv.RangeMax == nil {
		return tv
	}
	below := func(v float64) bool { return tv.RangeMin != nil && v < *tv.RangeMin }
	above := func(v float64) bool { return tv.RangeMax != nil && v > *tv.RangeMax }
	if tv.Actual > 0 {
		switch {
		case below(tv.Actual):
			tv.Status = "below"
		case above(tv.Actual):
			tv.Status = "above"
		default:
			tv.Status = "in"
		}
	}
	pts, cv := a.series(key)
	var in, lo, hi float64
	for i, q := range pts {
		if q[0] >= p.EndS {
			break
		}
		w := math.Min(cv[i], p.EndS) - math.Max(q[0], p.StartS)
		if w <= 0 {
			continue
		}
		switch {
		case below(q[1]):
			lo += w
		case above(q[1]):
			hi += w
		default:
			in += w
		}
	}
	if tot := in + lo + hi; tot > 0 {
		pct := func(v float64) float64 { return math.Round(v/tot*1000) / 10 }
		tv.InPct, tv.BelowPct, tv.AbovePct, tv.CoveredS = pct(in), pct(lo), pct(hi), math.Round(tot)
	}
	return tv
}

// resample averages series into buckets of `step` seconds, as older phones
// sent them: a reading at t covers the bucket from t; the distance is the
// running total at the end of each bucket.
func resample(in store.Series, step float64) store.Series {
	out := store.Series{}
	for k, pts := range in {
		if len(pts) == 0 {
			continue
		}
		last := pts[len(pts)-1][0]
		n := int(last/step) + 1
		if k == "distance" {
			xs, ys := make([]float64, len(pts)), make([]float64, len(pts))
			for i, p := range pts {
				xs[i], ys[i] = p[0], p[1]
			}
			r := [][2]float64{{0, interp(xs, ys, 0)}}
			for i := 1; i <= n; i++ {
				t := math.Min(float64(i)*step, last)
				r = append(r, [2]float64{float64(i) * step, math.Round(interp(xs, ys, t)*10) / 10})
			}
			out[k] = r
			continue
		}
		sums, counts := make([]float64, n), make([]float64, n)
		for _, p := range pts {
			i := int(p[0] / step)
			if i >= 0 && i < n {
				sums[i] += p[1]
				counts[i]++
			}
		}
		r := [][2]float64{}
		for i := range sums {
			if counts[i] > 0 {
				r = append(r, [2]float64{float64(i) * step, math.Round(sums[i]/counts[i]*100) / 100})
			}
		}
		out[k] = r
	}
	return out
}

// splits cuts the workout every `size` metres; the last one is kept when it
// is at least a tenth of a split.
func (a *along) splits(size float64) []partView {
	out := []partView{}
	if a.from == "" {
		return out
	}
	total := a.ds[len(a.ds)-1]
	for n, d0 := 1, 0.0; d0 < total && n <= 1000; n++ {
		d1 := math.Min(total, float64(n)*size)
		if d1-d0 < size/10 && n > 1 {
			break
		}
		p := a.part(store.WorkoutSegment{Kind: "split"}, a.timeAt(d0), a.timeAt(d1))
		p.N = n
		p.DistanceM = math.Round(d1 - d0)
		if p.DurationS > 0 && p.DistanceM > 0 {
			p.SpeedMS = round2(p.DistanceM / p.DurationS)
			p.PaceSKm = math.Round(p.DurationS / (p.DistanceM / 1000))
		}
		out = append(out, p)
		d0 = d1
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// zonesFor reads the person's zones. Heart rate zones not set are worked out
// as 60, 70, 80 and 90% of the heart rate reserve (max − resting, the way
// Apple Watch places them) from the max heart rate set, else 220 − age, and the
// latest resting heart rate; without one, of the max heart rate alone.
func (s *Server) zonesFor(ctx context.Context, uid int64) zones {
	z := zones{}
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return z
	}
	z.Power = ascending(st.PowerZones)
	if hr := ascending(st.HRZones); len(hr) > 0 {
		z.HR, z.HRFrom = hr, "settings"
		return z
	}
	max := st.MaxHR
	if max <= 0 && len(st.BirthDate) >= 10 {
		if b, err := time.Parse("2006-01-02", st.BirthDate[:10]); err == nil {
			age := time.Since(b).Hours() / 24 / 365.25
			if age >= 10 && age < 110 {
				max = math.Round(220 - age)
			}
		}
	}
	if max <= 0 {
		return z
	}
	rest := 0.0
	if p, err := s.store.LatestDaily(ctx, uid, "resting_hr"); err == nil && p.Value > 30 && p.Value < max {
		rest = p.Value
	}
	for _, f := range []float64{0.6, 0.7, 0.8, 0.9} {
		z.HR = append(z.HR, math.Round(rest+f*(max-rest)))
	}
	z.HRFrom = "estimated"
	return z
}

// ascending keeps zone tops that rise.
func ascending(v []float64) []float64 {
	var out []float64
	for _, x := range v {
		if x > 0 && (len(out) == 0 || x > out[len(out)-1]) {
			out = append(out, x)
		}
	}
	return out
}
