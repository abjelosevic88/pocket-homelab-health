package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- a workout's route ----
//
// The iPhone app sends the route Apple Health recorded with the workout (and
// the heart rate along it); that is kept in workout_tracks and wins. Without
// one, Movement (or Dawarich) keeps the phone's positions; Health asks for the
// ones recorded between a workout's start and end and draws them. That answer
// is cached a day per workout; "apply" copies the recorded distance onto a
// workout that has none.

type routeView struct {
	Connected bool         `json:"connected"`
	Via       string       `json:"via,omitempty"`
	Detail    string       `json:"detail,omitempty"`
	Points    [][2]float64 `json:"points"`
	N         int          `json:"n"`
	DistanceM float64      `json:"distance_m"`
	Bounds    [][2]float64 `json:"bounds,omitempty"`
	Start     string       `json:"start"`
	End       string       `json:"end"`
	FetchedAt string       `json:"fetched_at"`
	OpenURL   string       `json:"open_url,omitempty"`
	// "phone" (Apple Health's route, sent with the workout) or "movement"
	Source string `json:"source"`
	// The phone's route with altitude and time: [lat, lng, altitude m, seconds from the start]
	Track     [][4]float64 `json:"track,omitempty"`
	HeartRate [][2]float64 `json:"heart_rate,omitempty"`
	Place     string       `json:"place,omitempty"`
}

func routeKey(uid, wid int64) string { return fmt.Sprintf("route:%d:%d", uid, wid) }

// workoutRoute returns the phone's route, else the cached one or Movement's.
func (s *Server) workoutRoute(ctx context.Context, uid int64, wk *store.Workout, fresh bool) (*routeView, error) {
	track, err := s.store.WorkoutTrack(ctx, wk.ID)
	if err != nil {
		return nil, err
	}
	if track != nil && len(track.Route) > 1 {
		return s.phoneRoute(ctx, uid, wk, track), nil
	}
	rv, err := s.movementRoute(ctx, uid, wk, fresh)
	if err != nil {
		return nil, err
	}
	if track != nil {
		rv.HeartRate = track.HeartRate
	}
	return rv, nil
}

// phoneRoute draws what Apple Health recorded; the town it started in is
// looked up once and kept on the workout.
func (s *Server) phoneRoute(ctx context.Context, uid int64, wk *store.Workout, t *store.WorkoutTrack) *routeView {
	rv := &routeView{Connected: true, Via: "Apple Health", Source: "phone", Start: wk.StartAt, End: wk.EndAt, FetchedAt: t.UpdatedAt,
		Track: t.Route, HeartRate: t.HeartRate, N: len(t.Route), Place: wk.Place}
	rv.Points = make([][2]float64, 0, len(t.Route))
	minLat, minLng, maxLat, maxLng := 90.0, 180.0, -90.0, -180.0
	for i, p := range t.Route {
		if i > 0 {
			rv.DistanceM += haversine(t.Route[i-1][0], t.Route[i-1][1], p[0], p[1])
		}
		rv.Points = append(rv.Points, [2]float64{p[0], p[1]})
		minLat, maxLat = math.Min(minLat, p[0]), math.Max(maxLat, p[0])
		minLng, maxLng = math.Min(minLng, p[1]), math.Max(maxLng, p[1])
	}
	rv.DistanceM = math.Round(rv.DistanceM)
	rv.Bounds = [][2]float64{{minLat, minLng}, {maxLat, maxLng}}
	if rv.Place == "" {
		if place := reversePlace(ctx, t.Route[0][0], t.Route[0][1]); place != "" {
			rv.Place = place
			_ = s.store.SetWorkoutPlace(ctx, uid, wk.ID, place)
		}
	}
	return rv
}

// movementRoute returns the cached route or fetches it from Movement.
func (s *Server) movementRoute(ctx context.Context, uid int64, wk *store.Workout, fresh bool) (*routeView, error) {
	key := routeKey(uid, wk.ID)
	if !fresh {
		if v, at, ok := s.store.CacheGet(ctx, key); ok {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 24*time.Hour {
				var rv routeView
				if json.Unmarshal([]byte(v), &rv) == nil {
					return &rv, nil
				}
			}
		}
	}
	rv := &routeView{Points: [][2]float64{}, Source: "movement", Start: wk.StartAt, End: wk.EndAt, FetchedAt: store.Now()}
	conn, err := s.clients(ctx, uid).conn(ctx, appMovement, scopeMovementPoints)
	if err != nil {
		if isNotConnected(err) {
			rv.Detail = err.Error()
			return rv, nil
		}
		return nil, err
	}
	rv.Connected, rv.Via, rv.OpenURL = true, conn.via, conn.public
	start, err := time.Parse(time.RFC3339, wk.StartAt)
	if err != nil {
		return nil, fmt.Errorf("workout start: %w", err)
	}
	end := start.Add(time.Duration(wk.DurationS) * time.Second)
	if e, err := time.Parse(time.RFC3339, wk.EndAt); err == nil && e.After(start) {
		end = e
	}
	// a minute of slack each side: the watch starts the workout a moment after the phone's first fix
	pts, err := conn.client.Points(ctx, start.Add(-time.Minute), end.Add(time.Minute), 20000)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", appMovement.name, err)
	}
	rv.Points, rv.DistanceM = simplify(pts)
	rv.N = len(pts)
	if len(rv.Points) > 0 {
		minLat, minLng, maxLat, maxLng := 90.0, 180.0, -90.0, -180.0
		for _, p := range rv.Points {
			minLat, maxLat = math.Min(minLat, p[0]), math.Max(maxLat, p[0])
			minLng, maxLng = math.Min(minLng, p[1]), math.Max(maxLng, p[1])
		}
		rv.Bounds = [][2]float64{{minLat, minLng}, {maxLat, maxLng}}
	}
	if b, err := json.Marshal(rv); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return rv, nil
}

// simplify drops GPS jitter (points under 3 m apart) and implausible jumps
// (over 50 m/s), returns the polyline and its length in metres.
func simplify(pts []pocket.Point) ([][2]float64, float64) {
	out := make([][2]float64, 0, len(pts))
	dist := 0.0
	var last *pocket.Point
	for i := range pts {
		p := pts[i]
		if p.Lat == 0 && p.Lng == 0 {
			continue
		}
		if last != nil {
			d := haversine(last.Lat, last.Lng, p.Lat, p.Lng)
			dt := float64(p.TS - last.TS)
			if d < 3 {
				continue
			}
			if dt > 0 && d/dt > 50 {
				continue
			}
			dist += d
		}
		out = append(out, [2]float64{round6(p.Lat), round6(p.Lng)})
		last = &pts[i]
	}
	return out, math.Round(dist)
}

// reverseGeocodeURL names the town at a coordinate (OpenStreetMap's Nominatim);
// empty turns the lookup off (tests).
var reverseGeocodeURL = "https://nominatim.openstreetmap.org/reverse"

// reversePlace is the town or city at a coordinate, or "" when it cannot be told.
func reversePlace(ctx context.Context, lat, lng float64) string {
	if reverseGeocodeURL == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("lat", fmt.Sprintf("%.5f", lat))
	q.Set("lon", fmt.Sprintf("%.5f", lng))
	q.Set("zoom", "10")
	q.Set("format", "jsonv2")
	q.Set("accept-language", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reverseGeocodeURL+"?"+q.Encode(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "pocket-health/"+Version)
	res, err := weatherHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return ""
	}
	var body struct {
		Name    string `json:"name"`
		Address struct {
			City, Town, Village, Municipality string
		} `json:"address"`
	}
	if json.NewDecoder(res.Body).Decode(&body) != nil {
		return ""
	}
	for _, v := range []string{body.Address.City, body.Address.Town, body.Address.Village, body.Address.Municipality, body.Name} {
		if v != "" {
			return v
		}
	}
	return ""
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// haversine is the distance in metres between two coordinates.
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	toRad := math.Pi / 180
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (s *Server) handleWorkoutRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wk, err := s.store.GetWorkout(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	rv, err := s.workoutRoute(ctx, uid, wk, queryBool(r, "fresh"))
	if err != nil {
		writeJSON(w, http.StatusOK, routeView{Connected: true, Detail: err.Error(), Points: [][2]float64{}, Source: "movement", Start: wk.StartAt, End: wk.EndAt})
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

// handleWorkoutRouteApply copies the recorded distance onto the workout.
func (s *Server) handleWorkoutRouteApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wk, err := s.store.GetWorkout(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	rv, err := s.workoutRoute(ctx, uid, wk, false)
	if err != nil {
		s.fail(w, err, "route")
		return
	}
	if !rv.Connected || rv.DistanceM <= 0 {
		writeErr(w, http.StatusBadRequest, "no recorded distance for this workout")
		return
	}
	wk.DistanceM = rv.DistanceM
	if err := s.store.UpdateWorkout(ctx, uid, wk); err != nil {
		s.fail(w, err, "update workout")
		return
	}
	writeJSON(w, http.StatusOK, wk)
}
