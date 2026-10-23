package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/ingest"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

func (s *Server) handleListWorkouts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := "", ""
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
		from, to = dateRange(r, 30)
	}
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	list, err := s.store.ListWorkouts(ctx, actorID(ctx), store.WorkoutFilter{From: from, To: to, Kind: r.URL.Query().Get("kind"), Limit: limit, Offset: queryInt(r, "offset")})
	if err != nil {
		s.fail(w, err, "workouts")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleWorkoutKinds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, store.WorkoutKinds)
}

func (s *Server) handleWorkoutStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := rangeDays(r.URL.Query().Get("range"))
	to := store.Today()
	from := store.AddDays(to, -(days - 1))
	stats, err := s.store.WorkoutStats(ctx, actorID(ctx), from, to)
	if err != nil {
		s.fail(w, err, "workout stats")
		return
	}
	daysMap, _ := s.store.WorkoutDays(ctx, actorID(ctx), from, to)
	// per week count
	weekly := map[string]int{}
	for d, p := range daysMap {
		t, _ := store.ParseDate(d)
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		mon := t.AddDate(0, 0, -(wd - 1)).Format("2006-01-02")
		weekly[mon] += p.N
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "by_kind": stats, "days": daysMap, "weekly": weekly})
}

type workoutInput struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Start      string   `json:"start_at"`
	End        string   `json:"end_at"`
	DurationS  int      `json:"duration_s"`
	DurationM  float64  `json:"duration_min"`
	DistanceM  float64  `json:"distance_m"`
	DistanceKm float64  `json:"distance_km"`
	EnergyKcal float64  `json:"energy_kcal"`
	AvgHR      float64  `json:"avg_hr"`
	MaxHR      float64  `json:"max_hr"`
	ElevationM float64  `json:"elevation_m"`
	Steps      int      `json:"steps"`
	Notes      string   `json:"notes"`
	Temp       *float64 `json:"temperature"`
}

func (in *workoutInput) apply(wk *store.Workout) error {
	if in.Kind != "" {
		wk.Kind = strings.ToLower(in.Kind)
		if store.WorkoutKindName(wk.Kind) == "" {
			return fmt.Errorf("unknown workout kind")
		}
	}
	if wk.Kind == "" {
		wk.Kind = "other"
	}
	wk.Name = strings.TrimSpace(in.Name)
	start, ok := ingest.ParseTime(in.Start)
	if !ok {
		return fmt.Errorf("start_at must be a date/time")
	}
	wk.StartAt = ingest.RFC(start)
	if in.DurationS == 0 && in.DurationM > 0 {
		in.DurationS = int(in.DurationM * 60)
	}
	if e, ok := ingest.ParseTime(in.End); ok && e.After(start) {
		wk.EndAt = ingest.RFC(e)
		wk.DurationS = int(e.Sub(start).Seconds())
	} else if in.DurationS > 0 {
		wk.DurationS = in.DurationS
		wk.EndAt = ingest.RFC(start.Add(time.Duration(in.DurationS) * time.Second))
	} else {
		return fmt.Errorf("give an end time or a duration")
	}
	if in.DistanceKm > 0 {
		in.DistanceM = in.DistanceKm * 1000
	}
	wk.DistanceM, wk.EnergyKcal, wk.AvgHR, wk.MaxHR, wk.ElevationM, wk.Steps = in.DistanceM, in.EnergyKcal, in.AvgHR, in.MaxHR, in.ElevationM, in.Steps
	wk.Notes = strings.TrimSpace(in.Notes)
	wk.Temperature = in.Temp
	return nil
}

func (s *Server) handleCreateWorkout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in workoutInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	wk := store.Workout{Source: "manual"}
	if app := linkApp(ctx); app != "" {
		wk.Source = app
	}
	if err := in.apply(&wk); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	b := &ingest.Batch{Workouts: []store.Workout{wk}}
	if _, err := s.applyBatch(ctx, uid, b, ""); err != nil {
		s.fail(w, err, "workout")
		return
	}
	list, _ := s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{From: wk.Date, To: wk.Date})
	for _, x := range list {
		if x.StartAt == wk.StartAt && x.Kind == wk.Kind && x.Source == wk.Source {
			writeJSON(w, http.StatusCreated, x)
			return
		}
	}
	writeJSON(w, http.StatusCreated, wk)
}

func (s *Server) handleGetWorkout(w http.ResponseWriter, r *http.Request) {
	wk, err := s.store.GetWorkout(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	writeJSON(w, http.StatusOK, wk)
}

func (s *Server) handleUpdateWorkout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wk, err := s.store.GetWorkout(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	var in workoutInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// partial: fill what was not sent from the row
	if !keys["start_at"] {
		in.Start = wk.StartAt
	}
	if !keys["end_at"] && !keys["duration_s"] && !keys["duration_min"] {
		in.End = wk.EndAt
		in.DurationS = wk.DurationS
	}
	if !keys["kind"] {
		in.Kind = wk.Kind
	}
	if !keys["name"] {
		in.Name = wk.Name
	}
	if !keys["distance_m"] && !keys["distance_km"] {
		in.DistanceM = wk.DistanceM
	}
	if !keys["energy_kcal"] {
		in.EnergyKcal = wk.EnergyKcal
	}
	if !keys["avg_hr"] {
		in.AvgHR = wk.AvgHR
	}
	if !keys["max_hr"] {
		in.MaxHR = wk.MaxHR
	}
	if !keys["elevation_m"] {
		in.ElevationM = wk.ElevationM
	}
	if !keys["steps"] {
		in.Steps = wk.Steps
	}
	if !keys["notes"] {
		in.Notes = wk.Notes
	}
	if !keys["temperature"] {
		in.Temp = wk.Temperature
	}
	if err := in.apply(wk); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateWorkout(ctx, uid, wk); err != nil {
		s.fail(w, err, "update workout")
		return
	}
	writeJSON(w, http.StatusOK, wk)
}

func (s *Server) handleDeleteWorkout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWorkout(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete workout")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleWorkoutHabit ticks a habit for this workout now: {"habit_id": 3}.
func (s *Server) handleWorkoutHabit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wk, err := s.store.GetWorkout(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "workout")
		return
	}
	var in struct {
		HabitID int64 `json:"habit_id"`
	}
	if err := decode(r, &in); err != nil || in.HabitID <= 0 {
		writeErr(w, http.StatusBadRequest, "habit_id is required")
		return
	}
	if err := s.tickHabit(ctx, uid, in.HabitID, wk.Date); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = s.store.SetWorkoutHabit(ctx, uid, wk.ID, in.HabitID, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- sleep ----

func (s *Server) handleListSleep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := "", ""
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" || r.URL.Query().Get("range") != "" {
		days := rangeDays(r.URL.Query().Get("range"))
		from, to = dateRange(r, days)
	}
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 1000 {
		limit = 60
	}
	list, err := s.store.ListSleep(ctx, actorID(ctx), from, to, limit)
	if err != nil {
		s.fail(w, err, "sleep")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateSleep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Start     string  `json:"start_at"`
		End       string  `json:"end_at"`
		AsleepMin float64 `json:"asleep_min"`
		AwakeMin  float64 `json:"awake_min"`
		DeepMin   float64 `json:"deep_min"`
		RemMin    float64 `json:"rem_min"`
		Note      string  `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	start, ok1 := ingest.ParseTime(in.Start)
	end, ok2 := ingest.ParseTime(in.End)
	if !ok1 || !ok2 || !end.After(start) || end.Sub(start) > 24*time.Hour {
		writeErr(w, http.StatusBadRequest, "start and end must be date/times, at most a day apart")
		return
	}
	x := store.SleepSession{StartAt: ingest.RFC(start), EndAt: ingest.RFC(end), Source: "manual", Note: strings.TrimSpace(in.Note),
		AsleepS: int(in.AsleepMin * 60), AwakeS: int(in.AwakeMin * 60), DeepS: int(in.DeepMin * 60), RemS: int(in.RemMin * 60)}
	if x.AsleepS == 0 {
		x.AsleepS = int(end.Sub(start).Seconds()) - x.AwakeS
	}
	x.InBedS = int(end.Sub(start).Seconds())
	if app := linkApp(ctx); app != "" {
		x.Source = app
	}
	b := &ingest.Batch{Sleep: []store.SleepSession{x}}
	if _, err := s.applyBatch(ctx, uid, b, ""); err != nil {
		s.fail(w, err, "sleep")
		return
	}
	list, _ := s.store.ListSleep(ctx, uid, x.Date, x.Date, 5)
	for _, n := range list {
		if n.StartAt == x.StartAt && n.Source == x.Source {
			writeJSON(w, http.StatusCreated, n)
			return
		}
	}
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) handleDeleteSleep(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSleep(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete sleep")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
