package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const workoutCols = `id, user_id, kind, name, start_at, end_at, date, duration_s, distance_m, energy_kcal, avg_hr, max_hr, elevation_m, steps, temperature, humidity, source, device, external_id, notes, habit_id, habit_marked_at, habit_error, created_at, updated_at`

func scanWorkout(row interface{ Scan(...any) error }) (Workout, error) {
	var w Workout
	var temp, hum sql.NullFloat64
	var habit sql.NullInt64
	if err := row.Scan(&w.ID, &w.UserID, &w.Kind, &w.Name, &w.StartAt, &w.EndAt, &w.Date, &w.DurationS, &w.DistanceM, &w.EnergyKcal, &w.AvgHR, &w.MaxHR, &w.ElevationM, &w.Steps, &temp, &hum, &w.Source, &w.Device, &w.ExternalID, &w.Notes, &habit, &w.HabitMarkedAt, &w.HabitError, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return w, notFound(err)
	}
	w.Temperature, w.Humidity = nf(temp), nf(hum)
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
			_, err = s.DB.ExecContext(ctx, `UPDATE workouts SET name = ?, end_at = ?, date = ?, duration_s = ?, distance_m = ?, energy_kcal = ?, avg_hr = ?, max_hr = ?, elevation_m = ?, steps = ?, temperature = ?, humidity = ?, device = ?, external_id = ?,
				notes = CASE WHEN ? <> '' THEN ? ELSE notes END, updated_at = ? WHERE id = ?`,
				w.Name, w.EndAt, w.Date, w.DurationS, w.DistanceM, w.EnergyKcal, w.AvgHR, w.MaxHR, w.ElevationM, w.Steps, pf(w.Temperature), pf(w.Humidity), w.Device, w.ExternalID, w.Notes, w.Notes, Now(), id)
			if err != nil {
				return n, fresh, err
			}
			w.ID = id
			n++
			continue
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO workouts (user_id, kind, name, start_at, end_at, date, duration_s, distance_m, energy_kcal, avg_hr, max_hr, elevation_m, steps, temperature, humidity, source, device, external_id, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uid, w.Kind, w.Name, w.StartAt, w.EndAt, w.Date, w.DurationS, w.DistanceM, w.EnergyKcal, w.AvgHR, w.MaxHR, w.ElevationM, w.Steps, pf(w.Temperature), pf(w.Humidity), w.Source, w.Device, w.ExternalID, w.Notes)
		if err != nil {
			return n, fresh, err
		}
		w.ID, _ = res.LastInsertId()
		fresh = append(fresh, w.ID)
		n++
	}
	return n, fresh, nil
}

// WorkoutFilter narrows a listing.
type WorkoutFilter struct {
	From, To string
	Kind     string
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
