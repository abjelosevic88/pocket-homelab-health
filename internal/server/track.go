package server

import (
	"math"
	"net/http"
	"sort"

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
	UpdatedAt    string `json:"updated_at,omitempty"`
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
	tv := buildTrack(t, unit)
	// another app reading through a link gets the numbers, not where you were
	if linkApp(ctx) != "" {
		tv.Route = [][4]float64{}
	}
	writeJSON(w, http.StatusOK, tv)
}

func buildTrack(t *store.WorkoutTrack, unit string) trackView {
	tv := trackView{Route: [][4]float64{}, Series: store.Series{}, Segments: []partView{}, Splits: []partView{}, SplitUnit: unit}
	if t == nil {
		return tv
	}
	tv.Route, tv.Series, tv.UpdatedAt = t.Route, t.Series, t.UpdatedAt
	a := newAlong(t)
	tv.DistanceFrom = a.from
	segs := append([]store.WorkoutSegment(nil), t.Segments...)
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].StartS < segs[j].StartS })
	for _, sg := range segs {
		tv.Segments = append(tv.Segments, a.part(sg, sg.StartS, sg.EndS))
	}
	size := 1000.0
	if unit == "mi" {
		size = 1609.344
	}
	tv.Splits = a.splits(size)
	return tv
}

// along answers "how far by then" and "what was recorded between then and then".
type along struct {
	t      *store.WorkoutTrack
	ts, ds []float64 // a running distance over time, from the device's series or the route
	from   string
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

// stat is the average and the highest of a series between two moments; a
// part shorter than the spacing of the series takes the nearest reading.
func (a *along) stat(key string, from, to float64) (avg, max float64) {
	pts := a.t.Series[key]
	if len(pts) == 0 {
		return 0, 0
	}
	sum, n := 0.0, 0
	for _, p := range pts {
		if p[0] >= from && p[0] < to {
			sum += p[1]
			n++
			max = math.Max(max, p[1])
		}
	}
	if n > 0 {
		return sum / float64(n), max
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
