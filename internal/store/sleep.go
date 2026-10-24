package store

import (
	"context"
	"fmt"
	"time"
)

const sleepCols = `id, user_id, start_at, end_at, date, in_bed_s, asleep_s, core_s, deep_s, rem_s, awake_s, source, device, note`

func scanSleep(row interface{ Scan(...any) error }) (SleepSession, error) {
	var x SleepSession
	if err := row.Scan(&x.ID, &x.UserID, &x.StartAt, &x.EndAt, &x.Date, &x.InBedS, &x.AsleepS, &x.CoreS, &x.DeepS, &x.RemS, &x.AwakeS, &x.Source, &x.Device, &x.Note); err != nil {
		return x, notFound(err)
	}
	return x, nil
}

// UpsertSleep writes nights (by start+source) and rolls their days up.
func (s *Store) UpsertSleep(ctx context.Context, uid int64, in []SleepSession) (int, []int64, error) {
	n := 0
	var fresh []int64
	dates := map[string]bool{}
	for i := range in {
		x := &in[i]
		if x.StartAt == "" || x.EndAt == "" {
			continue
		}
		if x.Date == "" {
			x.Date = DateOf(x.EndAt)
		}
		if x.Source == "" {
			x.Source = "manual"
		}
		if x.AsleepS == 0 {
			x.AsleepS = x.CoreS + x.DeepS + x.RemS
		}
		if x.InBedS == 0 {
			x.InBedS = x.AsleepS + x.AwakeS
		}
		var id int64
		var oldDate string
		err := s.DB.QueryRowContext(ctx, `SELECT id, date FROM sleep_sessions WHERE user_id = ? AND start_at = ? AND source = ?`, uid, x.StartAt, x.Source).Scan(&id, &oldDate)
		if err != nil {
			// The same night sent again with a different start (the watch's first
			// minutes arrived later) replaces the earlier copy from that source.
			if oid, od, ok := s.overlappingSleep(ctx, uid, x); ok {
				id, oldDate, err = oid, od, nil
			}
		}
		if err == nil {
			dates[oldDate] = true
			if _, err := s.DB.ExecContext(ctx, `UPDATE sleep_sessions SET start_at = ?, end_at = ?, date = ?, in_bed_s = ?, asleep_s = ?, core_s = ?, deep_s = ?, rem_s = ?, awake_s = ?, device = ?, note = CASE WHEN ? <> '' THEN ? ELSE note END WHERE id = ?`,
				x.StartAt, x.EndAt, x.Date, x.InBedS, x.AsleepS, x.CoreS, x.DeepS, x.RemS, x.AwakeS, x.Device, x.Note, x.Note, id); err != nil {
				return n, fresh, err
			}
			x.ID = id
		} else {
			res, err := s.DB.ExecContext(ctx, `INSERT INTO sleep_sessions (user_id, start_at, end_at, date, in_bed_s, asleep_s, core_s, deep_s, rem_s, awake_s, source, device, note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uid, x.StartAt, x.EndAt, x.Date, x.InBedS, x.AsleepS, x.CoreS, x.DeepS, x.RemS, x.AwakeS, x.Source, x.Device, x.Note)
			if err != nil {
				return n, fresh, err
			}
			x.ID, _ = res.LastInsertId()
			fresh = append(fresh, x.ID)
		}
		dates[x.Date] = true
		n++
	}
	for d := range dates {
		if err := s.rollupSleepDay(ctx, uid, d); err != nil {
			return n, fresh, err
		}
	}
	return n, fresh, nil
}

// overlappingSleep finds a session from the same source whose time overlaps
// x (dates a day either side of x's morning are enough to look at).
func (s *Store) overlappingSleep(ctx context.Context, uid int64, x *SleepSession) (int64, string, bool) {
	start, err1 := time.Parse(time.RFC3339, x.StartAt)
	end, err2 := time.Parse(time.RFC3339, x.EndAt)
	if err1 != nil || err2 != nil {
		return 0, "", false
	}
	day, err := time.Parse("2006-01-02", x.Date)
	if err != nil {
		return 0, "", false
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, date, start_at, end_at FROM sleep_sessions WHERE user_id = ? AND source = ? AND date >= ? AND date <= ? ORDER BY id`,
		uid, x.Source, day.AddDate(0, 0, -1).Format("2006-01-02"), day.AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		return 0, "", false
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var date, sa, ea string
		if rows.Scan(&id, &date, &sa, &ea) != nil {
			continue
		}
		ps, e1 := time.Parse(time.RFC3339, sa)
		oe, e2 := time.Parse(time.RFC3339, ea)
		if e1 == nil && e2 == nil && ps.Before(end) && oe.After(start) {
			return id, date, true
		}
	}
	return 0, "", false
}

// rollupSleepDay writes the sleep_* daily rows for a morning. Several
// sessions on one date (a nap) add up; several SOURCES for the same night
// do not: the longest source's night wins.
func (s *Store) rollupSleepDay(ctx context.Context, uid int64, date string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT source, in_bed_s, asleep_s, core_s, deep_s, rem_s, awake_s FROM sleep_sessions WHERE user_id = ? AND date = ?`, uid, date)
	if err != nil {
		return err
	}
	type acc struct{ bed, asleep, core, deep, rem, awake int }
	by := map[string]*acc{}
	for rows.Next() {
		var src string
		var a acc
		if err := rows.Scan(&src, &a.bed, &a.asleep, &a.core, &a.deep, &a.rem, &a.awake); err != nil {
			rows.Close()
			return err
		}
		if by[src] == nil {
			by[src] = &acc{}
		}
		t := by[src]
		t.bed += a.bed
		t.asleep += a.asleep
		t.core += a.core
		t.deep += a.deep
		t.rem += a.rem
		t.awake += a.awake
	}
	rows.Close()
	keys := []string{"sleep_asleep", "sleep_in_bed", "sleep_deep", "sleep_rem", "sleep_core", "sleep_awake"}
	if len(by) == 0 {
		for _, k := range keys {
			if _, err := s.DB.ExecContext(ctx, `DELETE FROM daily WHERE user_id = ? AND metric = ? AND date = ?`, uid, k, date); err != nil {
				return err
			}
		}
		return nil
	}
	var best *acc
	for _, a := range by {
		if best == nil || a.asleep > best.asleep {
			best = a
		}
	}
	vals := map[string]int{"sleep_asleep": best.asleep, "sleep_in_bed": best.bed, "sleep_deep": best.deep, "sleep_rem": best.rem, "sleep_core": best.core, "sleep_awake": best.awake}
	for _, k := range keys {
		v := vals[k]
		if v == 0 && k != "sleep_asleep" && k != "sleep_in_bed" {
			if _, err := s.DB.ExecContext(ctx, `DELETE FROM daily WHERE user_id = ? AND metric = ? AND date = ?`, uid, k, date); err != nil {
				return err
			}
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO daily (user_id, metric, date, value, n, updated_at) VALUES (?, ?, ?, ?, 1, ?)
			ON CONFLICT(user_id, metric, date) DO UPDATE SET value = excluded.value, n = 1, updated_at = excluded.updated_at`, uid, k, date, float64(v)/60, Now()); err != nil {
			return err
		}
	}
	return nil
}

// RollupSleepAll recomputes every night's daily rows.
func (s *Store) RollupSleepAll(ctx context.Context, uid int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT date FROM sleep_sessions WHERE user_id = ? UNION SELECT date FROM daily WHERE user_id = ? AND metric = 'sleep_asleep'`, uid, uid)
	if err != nil {
		return err
	}
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return err
		}
		dates = append(dates, d)
	}
	rows.Close()
	for _, d := range dates {
		if err := s.rollupSleepDay(ctx, uid, d); err != nil {
			return err
		}
	}
	return nil
}

// ListSleep lists nights in a range, newest first.
func (s *Store) ListSleep(ctx context.Context, uid int64, from, to string, limit int) ([]SleepSession, error) {
	q := `SELECT ` + sleepCols + ` FROM sleep_sessions WHERE user_id = ?`
	args := []any{uid}
	if from != "" {
		q += ` AND date >= ?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND date <= ?`
		args = append(args, to)
	}
	q += ` ORDER BY start_at DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SleepSession{}
	for rows.Next() {
		x, err := scanSleep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetSleep returns one night.
func (s *Store) GetSleep(ctx context.Context, uid, id int64) (*SleepSession, error) {
	x, err := scanSleep(s.DB.QueryRowContext(ctx, `SELECT `+sleepCols+` FROM sleep_sessions WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// DeleteSleep removes a night and rolls its day up again.
func (s *Store) DeleteSleep(ctx context.Context, uid, id int64) error {
	x, err := s.GetSleep(ctx, uid, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sleep_sessions WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	return s.rollupSleepDay(ctx, uid, x.Date)
}
