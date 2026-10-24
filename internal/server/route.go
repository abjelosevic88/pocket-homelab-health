package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- a workout's route from Pocket Movement ----
//
// Movement (or Dawarich) keeps the phone's positions; Health asks for the
// ones recorded between a workout's start and end and draws them. The
// answer is cached a day per workout; "apply" copies the recorded distance
// onto a workout that has none.

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
}

func routeKey(uid, wid int64) string { return fmt.Sprintf("route:%d:%d", uid, wid) }

// workoutRoute returns the cached route or fetches it from Movement.
func (s *Server) workoutRoute(ctx context.Context, uid int64, wk *store.Workout, fresh bool) (*routeView, error) {
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
	rv := &routeView{Points: [][2]float64{}, Start: wk.StartAt, End: wk.EndAt, FetchedAt: store.Now()}
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
		writeJSON(w, http.StatusOK, routeView{Connected: true, Detail: err.Error(), Points: [][2]float64{}, Start: wk.StartAt, End: wk.EndAt})
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
