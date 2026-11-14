package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// MaxSeriesPoints caps one series of one workout (eight hours at one reading a second).
const MaxSeriesPoints = 28800

// Clean sorts each series by time, drops readings before the start or not a
// number, and thins a series past MaxSeriesPoints evenly.
func (s Series) Clean() Series {
	out := Series{}
	for k, pts := range s {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		c := make([][2]float64, 0, len(pts))
		for _, p := range pts {
			if p[0] < 0 || math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) || math.IsInf(p[0], 0) {
				continue
			}
			c = append(c, p)
		}
		sort.SliceStable(c, func(i, j int) bool { return c[i][0] < c[j][0] })
		if n := len(c); n > MaxSeriesPoints {
			th := make([][2]float64, 0, MaxSeriesPoints)
			for i := 0; i < MaxSeriesPoints; i++ {
				th = append(th, c[i*n/MaxSeriesPoints])
			}
			c = th
		}
		if len(c) > 0 {
			out[k] = c
		}
	}
	return out
}

// Step works out the spacing the series were sent at: the smallest gap
// between two readings when every reading sits on a multiple of it (a phone
// that buckets every 5 s), else 0 (the device's own rate).
func (s Series) Step() float64 {
	gap := math.Inf(1)
	for _, pts := range s {
		for i := 1; i < len(pts); i++ {
			if d := pts[i][0] - pts[i-1][0]; d > 1e-6 && d < gap {
				gap = d
			}
		}
	}
	if math.IsInf(gap, 1) || gap < 1 {
		return 0
	}
	for _, pts := range s {
		for _, p := range pts {
			if r := math.Mod(p[0], gap); r > 1e-6 && gap-r > 1e-6 {
				return 0
			}
		}
	}
	return gap
}

const workoutCols = `id, user_id, kind, name, start_at, end_at, date, duration_s, distance_m, energy_kcal, total_kcal, avg_hr, max_hr, elevation_m, steps, temperature, humidity, place, effort, effort_estimated, plan_id, plan_name, source, device, external_id, notes, habit_id, habit_marked_at, habit_error, created_at, updated_at`

func scanWorkout(row interface{ Scan(...any) error }) (Workout, error) {
	var w Workout
	var temp, hum, effort, effortEst sql.NullFloat64
	var habit sql.NullInt64
	if err := row.Scan(&w.ID, &w.UserID, &w.Kind, &w.Name, &w.StartAt, &w.EndAt, &w.Date, &w.DurationS, &w.DistanceM, &w.EnergyKcal, &w.TotalKcal, &w.AvgHR, &w.MaxHR, &w.ElevationM, &w.Steps, &temp, &hum, &w.Place, &effort, &effortEst, &w.PlanID, &w.PlanName, &w.Source, &w.Device, &w.ExternalID, &w.Notes, &habit, &w.HabitMarkedAt, &w.HabitError, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return w, notFound(err)
	}
	w.Temperature, w.Humidity = nf(temp), nf(hum)
	w.Effort, w.EffortEstimated = nf(effort), nf(effortEst)
	if habit.Valid {
		h := habit.Int64
		w.HabitID = &h
	}
	return w, nil
}

// UpsertWorkouts writes workouts (insert or update by start+kind+source);
// returns the ids of the rows that are new.
func (s *Store) UpsertWorkouts(ctx context.Context, uid int64, in []Workout) (int, []int64, error) {
	var fresh []int64
	n := 0
	for i := range in {
		w := &in[i]
		if w.StartAt == "" {
			continue
		}
		if w.Date == "" {
			w.Date = DateOf(w.StartAt)
		}
		if w.Kind == "" {
			w.Kind = "other"
		}
		if w.Source == "" {
			w.Source = "manual"
		}
		w.PlanID, w.PlanName = strings.TrimSpace(w.PlanID), strings.TrimSpace(w.PlanName)
		if w.PlanName != "" {
			w.Name = w.PlanName
		}
		// Newer watches keep the climb on the segments rather than the workout: a
		// workout that arrives without one gets it from its route's altitudes.
		if w.ElevationM == 0 && len(w.Route) > 1 {
			w.ElevationM = Ascent(w.Route)
		}
		if w.DurationS == 0 && w.EndAt != "" {
			if a, err := time.Parse(time.RFC3339, w.StartAt); err == nil {
				if b, err := time.Parse(time.RFC3339, w.EndAt); err == nil && b.After(a) {
					w.DurationS = int(b.Sub(a).Seconds())
				}
			}
		}
		var id int64
		err := s.DB.QueryRowContext(ctx, `SELECT id FROM workouts WHERE user_id = ? AND start_at = ? AND kind = ? AND source = ?`, uid, w.StartAt, w.Kind, w.Source).Scan(&id)
		if err == nil {
			// a copy sent without its plan (an older phone) keeps the plan's name
			_, err = s.DB.ExecContext(ctx, `UPDATE workouts SET name = CASE WHEN ? = '' AND plan_name <> '' THEN name ELSE ? END,
				plan_id = CASE WHEN ? <> '' THEN ? ELSE plan_id END, plan_name = CASE WHEN ? <> '' THEN ? ELSE plan_name END, end_at = ?, date = ?, duration_s = ?, distance_m = ?, energy_kcal = ?, total_kcal = ?, avg_hr = ?, max_hr = ?, elevation_m = CASE WHEN ? > 0 THEN ? ELSE elevation_m END, steps = ?, temperature = ?, humidity = ?, device = ?, external_id = ?,
				effort = COALESCE(?, effort), effort_estimated = COALESCE(?, effort_estimated),
				place = CASE WHEN ? <> '' THEN ? ELSE place END, notes = CASE WHEN ? <> '' THEN ? ELSE notes END, updated_at = ? WHERE id = ?`,
				w.PlanName, w.Name, w.PlanID, w.PlanID, w.PlanName, w.PlanName, w.EndAt, w.Date, w.DurationS, w.DistanceM, w.EnergyKcal, w.TotalKcal, w.AvgHR, w.MaxHR, w.ElevationM, w.ElevationM, w.Steps, pf(w.Temperature), pf(w.Humidity), w.Device, w.ExternalID, pf(w.Effort), pf(w.EffortEstimated), w.Place, w.Place, w.Notes, w.Notes, Now(), id)
			if err != nil {
				return n, fresh, err
			}
			w.ID = id
			if err := s.putTrack(ctx, w); err != nil {
				return n, fresh, err
			}
			n++
			continue
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO workouts (user_id, kind, name, start_at, end_at, date, duration_s, distance_m, energy_kcal, total_kcal, avg_hr, max_hr, elevation_m, steps, temperature, humidity, place, effort, effort_estimated, plan_id, plan_name, source, device, external_id, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uid, w.Kind, w.Name, w.StartAt, w.EndAt, w.Date, w.DurationS, w.DistanceM, w.EnergyKcal, w.TotalKcal, w.AvgHR, w.MaxHR, w.ElevationM, w.Steps, pf(w.Temperature), pf(w.Humidity), w.Place, pf(w.Effort), pf(w.EffortEstimated), w.PlanID, w.PlanName, w.Source, w.Device, w.ExternalID, w.Notes)
		if err != nil {
			return n, fresh, err
		}
		w.ID, _ = res.LastInsertId()
		if err := s.putTrack(ctx, w); err != nil {
			return n, fresh, err
		}
		fresh = append(fresh, w.ID)
		n++
	}
	return n, fresh, nil
}

// putTrack keeps what was recorded along a workout. A copy sent without a part
// (Apple Health saves the route a moment after the workout) keeps the part
// already here.
func (s *Store) putTrack(ctx context.Context, w *Workout) error {
	// Segments sent as an empty list replace the ones here (a newer phone found none); left
	// out, they stay.
	if len(w.Route) == 0 && len(w.Series) == 0 && w.Segments == nil {
		return nil
	}
	enc := func(v any, empty bool) any {
		if empty {
			return nil
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	for i := range w.Segments {
		w.Segments[i].Target = w.Segments[i].Target.Normalize()
	}
	var step any
	if len(w.Series) > 0 {
		w.Series = w.Series.Clean()
		st := w.Series.Step()
		if w.SeriesStep != nil && *w.SeriesStep >= 0 {
			st = *w.SeriesStep
		}
		step = st
	}
	route, series, segs := enc(w.Route, len(w.Route) == 0), enc(w.Series, len(w.Series) == 0), enc(w.Segments, w.Segments == nil)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO workout_tracks (workout_id, route, series, segments, series_step, updated_at) VALUES (?, COALESCE(?, '[]'), COALESCE(?, '{}'), COALESCE(?, '[]'), COALESCE(?, 5), ?)
		ON CONFLICT(workout_id) DO UPDATE SET route = COALESCE(?, route), series = COALESCE(?, series), segments = COALESCE(?, segments), series_step = COALESCE(?, series_step), updated_at = excluded.updated_at`,
		w.ID, route, series, segs, step, Now(), route, series, segs, step)
	return err
}

// WorkoutTrack returns what was recorded along a workout, or nil.
func (s *Store) WorkoutTrack(ctx context.Context, workoutID int64) (*WorkoutTrack, error) {
	var route, series, segs string
	t := &WorkoutTrack{}
	err := s.DB.QueryRowContext(ctx, `SELECT route, series, segments, series_step, updated_at FROM workout_tracks WHERE workout_id = ?`, workoutID).Scan(&route, &series, &segs, &t.SeriesStep, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(route), &t.Route)
	_ = json.Unmarshal([]byte(series), &t.Series)
	_ = json.Unmarshal([]byte(segs), &t.Segments)
	if t.Route == nil {
		t.Route = [][4]float64{}
	}
	if t.Series == nil {
		t.Series = Series{}
	}
	if t.Segments == nil {
		t.Segments = []WorkoutSegment{}
	}
	return t, nil
}

// Ascent is the climb along a route, in metres: rises of 3 m or more over the
// last low point count, so GPS and barometer noise does not add up.
func Ascent(route [][4]float64) float64 {
	if len(route) < 2 {
		return 0
	}
	gain, ref := 0.0, route[0][2]
	for _, p := range route[1:] {
		switch {
		case p[2]-ref >= 3:
			gain += p[2] - ref
			ref = p[2]
		case p[2] < ref:
			ref = p[2]
		}
	}
	return float64(int(gain + 0.5))
}

// SetWorkoutPlace names the town a workout started in.
func (s *Store) SetWorkoutPlace(ctx context.Context, uid, id int64, place string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE workouts SET place = ? WHERE id = ? AND user_id = ?`, place, id, uid)
	return err
}

// WorkoutFilter narrows a listing.
type WorkoutFilter struct {
	From, To string
	Kind     string
	PlanID   string
	Limit    int
	Offset   int
}

// ListWorkouts lists a person's workouts, newest first.
func (s *Store) ListWorkouts(ctx context.Context, uid int64, f WorkoutFilter) ([]Workout, error) {
	q := `SELECT ` + workoutCols + ` FROM workouts WHERE user_id = ?`
	args := []any{uid}
	if f.From != "" {
		q += ` AND date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND date <= ?`
		args = append(args, f.To)
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.PlanID != "" {
		q += ` AND plan_id = ?`
		args = append(args, f.PlanID)
	}
	q += ` ORDER BY start_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d OFFSET %d`, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workout{}
	for rows.Next() {
		w, err := scanWorkout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWorkout returns one of the person's workouts.
func (s *Store) GetWorkout(ctx context.Context, uid, id int64) (*Workout, error) {
	w, err := scanWorkout(s.DB.QueryRowContext(ctx, `SELECT `+workoutCols+` FROM workouts WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// UpdateWorkout saves edits.
func (s *Store) UpdateWorkout(ctx context.Context, uid int64, w *Workout) error {
	w.Date = DateOf(w.StartAt)
	res, err := s.DB.ExecContext(ctx, `UPDATE workouts SET kind = ?, name = ?, start_at = ?, end_at = ?, date = ?, duration_s = ?, distance_m = ?, energy_kcal = ?, avg_hr = ?, max_hr = ?, elevation_m = ?, steps = ?, notes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		w.Kind, w.Name, w.StartAt, w.EndAt, w.Date, w.DurationS, w.DistanceM, w.EnergyKcal, w.AvgHR, w.MaxHR, w.ElevationM, w.Steps, w.Notes, Now(), w.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWorkout removes one.
func (s *Store) DeleteWorkout(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM workouts WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetWorkoutHabit records that a habit was (or could not be) ticked for it.
func (s *Store) SetWorkoutHabit(ctx context.Context, uid, id int64, habitID int64, errText string) error {
	marked := ""
	if errText == "" {
		marked = Now()
	}
	var h any
	if habitID > 0 {
		h = habitID
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE workouts SET habit_id = ?, habit_marked_at = ?, habit_error = ? WHERE id = ? AND user_id = ?`, h, marked, errText, id, uid)
	return err
}

// WorkoutsToMark lists recent workouts whose habit tick has not happened.
func (s *Store) WorkoutsToMark(ctx context.Context, uid int64, since string) ([]Workout, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+workoutCols+` FROM workouts WHERE user_id = ? AND date >= ? AND habit_marked_at = '' ORDER BY start_at`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workout
	for rows.Next() {
		w, err := scanWorkout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WorkoutKindStat sums a kind over a range.
type WorkoutKindStat struct {
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	DurationS  int     `json:"duration_s"`
	DistanceM  float64 `json:"distance_m"`
	EnergyKcal float64 `json:"energy_kcal"`
}

// WorkoutStats totals workouts per kind in a range.
func (s *Store) WorkoutStats(ctx context.Context, uid int64, from, to string) ([]WorkoutKindStat, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT kind, COUNT(1), COALESCE(SUM(duration_s),0), COALESCE(SUM(distance_m),0), COALESCE(SUM(energy_kcal),0) FROM workouts WHERE user_id = ? AND date >= ? AND date <= ? GROUP BY kind ORDER BY SUM(duration_s) DESC`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkoutKindStat{}
	for rows.Next() {
		var k WorkoutKindStat
		if err := rows.Scan(&k.Kind, &k.Count, &k.DurationS, &k.DistanceM, &k.EnergyKcal); err != nil {
			return nil, err
		}
		k.Name = WorkoutKindName(k.Kind)
		out = append(out, k)
	}
	return out, rows.Err()
}

// WorkoutDays returns per day the minutes and count of workouts in a range.
func (s *Store) WorkoutDays(ctx context.Context, uid int64, from, to string) (map[string]DailyPoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date, COALESCE(SUM(duration_s),0), COUNT(1) FROM workouts WHERE user_id = ? AND date >= ? AND date <= ? GROUP BY date`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DailyPoint{}
	for rows.Next() {
		var d string
		var secs, n int
		if err := rows.Scan(&d, &secs, &n); err != nil {
			return nil, err
		}
		out[d] = DailyPoint{Date: d, Value: float64(secs) / 60, N: n}
	}
	return out, rows.Err()
}
