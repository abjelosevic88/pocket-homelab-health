package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
)

// Touched is the set of (metric, date) a write changed, for the rollups.
type Touched map[string]map[string]bool

func (t Touched) add(metric, date string) {
	if t[metric] == nil {
		t[metric] = map[string]bool{}
	}
	t[metric][date] = true
}

// Merge folds another set in.
func (t Touched) Merge(o Touched) {
	for m, ds := range o {
		for d := range ds {
			t.add(m, d)
		}
	}
}

// Count is the number of (metric, date) pairs.
func (t Touched) Count() int {
	n := 0
	for _, ds := range t {
		n += len(ds)
	}
	return n
}

const sampleCols = `id, user_id, metric, start_at, end_at, date, value, value2, value_min, value_max, unit, source, device, note`

func scanSample(row interface{ Scan(...any) error }) (Sample, error) {
	var s Sample
	var v2, vmin, vmax sql.NullFloat64
	if err := row.Scan(&s.ID, &s.UserID, &s.Metric, &s.StartAt, &s.EndAt, &s.Date, &s.Value, &v2, &vmin, &vmax, &s.Unit, &s.Source, &s.Device, &s.Note); err != nil {
		return s, notFound(err)
	}
	s.Value2, s.ValueMin, s.ValueMax = nf(v2), nf(vmin), nf(vmax)
	return s, nil
}

func nf(n sql.NullFloat64) *float64 {
	if n.Valid {
		v := n.Float64
		return &v
	}
	return nil
}

func pf(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// DateOf is the local date of an RFC3339 time string (the offset in the
// string decides the day; without one the server's zone).
func DateOf(ts string) string {
	if len(ts) >= 10 {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.Format("2006-01-02")
		}
		return ts[:10]
	}
	return ts
}

// UpsertSamples writes samples (insert or update by metric+start+source+device)
// in one transaction and returns how many were written and what changed.
func (s *Store) UpsertSamples(ctx context.Context, uid int64, in []Sample) (int, Touched, error) {
	touched := Touched{}
	if len(in) == 0 {
		return 0, touched, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO samples (user_id, metric, start_at, end_at, date, value, value2, value_min, value_max, unit, source, device, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, metric, start_at, source, device) DO UPDATE SET end_at = excluded.end_at, date = excluded.date, value = excluded.value,
		value2 = excluded.value2, value_min = excluded.value_min, value_max = excluded.value_max, unit = excluded.unit,
		note = CASE WHEN excluded.note <> '' THEN excluded.note ELSE samples.note END`)
	if err != nil {
		return 0, nil, err
	}
	defer stmt.Close()
	n := 0
	for _, sm := range in {
		if sm.Metric == "" || sm.StartAt == "" {
			continue
		}
		if sm.Date == "" {
			sm.Date = DateOf(sm.StartAt)
		}
		if sm.Source == "" {
			sm.Source = "manual"
		}
		if _, err := stmt.ExecContext(ctx, uid, sm.Metric, sm.StartAt, sm.EndAt, sm.Date, sm.Value, pf(sm.Value2), pf(sm.ValueMin), pf(sm.ValueMax), sm.Unit, sm.Source, sm.Device, sm.Note); err != nil {
			return n, nil, err
		}
		n++
		touched.add(sm.Metric, sm.Date)
	}
	if err := tx.Commit(); err != nil {
		return n, nil, err
	}
	return n, touched, nil
}

// Rollup recomputes the daily rows for the touched (metric, date) pairs.
// prefer names a device whose samples win over other devices on summed
// metrics ("Apple Watch"); without it the device with the biggest day total wins.
func (s *Store) Rollup(ctx context.Context, uid int64, touched Touched, prefer string) error {
	for metric, dates := range touched {
		if strings.HasPrefix(metric, "sleep_") {
			continue // from the sessions
		}
		m := metrics.Get(metric)
		for date := range dates {
			if err := s.rollupDay(ctx, uid, m, date, prefer); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) rollupDay(ctx context.Context, uid int64, m metrics.Metric, date, prefer string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT value, value2, value_min, value_max, device, source, unit FROM samples WHERE user_id = ? AND metric = ? AND date = ? ORDER BY start_at, id`, uid, m.Key, date)
	if err != nil {
		return err
	}
	type rec struct {
		v, v2, lo, hi  sql.NullFloat64
		dev, src, unit string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.v, &r.v2, &r.lo, &r.hi, &r.dev, &r.src, &r.unit); err != nil {
			rows.Close()
			return err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if len(recs) == 0 {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM daily WHERE user_id = ? AND metric = ? AND date = ?`, uid, m.Key, date)
		return err
	}
	agg := m.Agg
	if !metrics.Known(m.Key) {
		agg = metrics.GuessAgg(recs[0].unit)
	}
	// Summed metrics: one device per day (two devices counting the same
	// steps would double them). The preferred device wins when it has data;
	// else the device with the largest total (Apple's own dedupe lands near that).
	if agg == metrics.Sum {
		totals := map[string]float64{}
		for _, r := range recs {
			totals[r.dev] += r.v.Float64
		}
		if len(totals) > 1 {
			win := ""
			if prefer != "" {
				for d := range totals {
					if strings.EqualFold(d, prefer) {
						win = d
					}
				}
			}
			if win == "" {
				best := -1.0
				for d, t := range totals {
					if t > best || (t == best && d < win) {
						best, win = t, d
					}
				}
			}
			var keep []rec
			for _, r := range recs {
				if r.dev == win {
					keep = append(keep, r)
				}
			}
			recs = keep
		}
	}
	vals := make([]float64, 0, len(recs))
	var v2s []float64
	var lo, hi *float64
	for _, r := range recs {
		vals = append(vals, r.v.Float64)
		if r.v2.Valid {
			v2s = append(v2s, r.v2.Float64)
		}
		l, h := r.v.Float64, r.v.Float64
		if r.lo.Valid {
			l = r.lo.Float64
		}
		if r.hi.Valid {
			h = r.hi.Float64
		}
		if lo == nil || l < *lo {
			lo = &l
		}
		if hi == nil || h > *hi {
			hi = &h
		}
	}
	mm := m
	mm.Agg = agg
	value := metrics.Aggregate(mm, vals)
	var value2 any
	if len(v2s) > 0 {
		v := metrics.Aggregate(mm, v2s)
		value2 = v
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO daily (user_id, metric, date, value, value2, min, max, n, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, metric, date) DO UPDATE SET value = excluded.value, value2 = excluded.value2, min = excluded.min, max = excluded.max, n = excluded.n, updated_at = excluded.updated_at`,
		uid, m.Key, date, value, value2, pf(lo), pf(hi), len(recs), Now())
	return err
}

// RollupAll recomputes every day of every metric a person has (after an import).
func (s *Store) RollupAll(ctx context.Context, uid int64, prefer string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT metric, date FROM samples WHERE user_id = ? UNION SELECT metric, date FROM daily WHERE user_id = ? AND metric NOT LIKE 'sleep\_%' ESCAPE '\'`, uid, uid)
	if err != nil {
		return err
	}
	t := Touched{}
	for rows.Next() {
		var m, d string
		if err := rows.Scan(&m, &d); err != nil {
			rows.Close()
			return err
		}
		t.add(m, d)
	}
	rows.Close()
	if err := s.Rollup(ctx, uid, t, prefer); err != nil {
		return err
	}
	return s.RollupSleepAll(ctx, uid)
}

// Daily returns a metric's days in a range (days without data are absent).
func (s *Store) Daily(ctx context.Context, uid int64, metric, from, to string) ([]DailyPoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date, value, value2, min, max, n FROM daily WHERE user_id = ? AND metric = ? AND date >= ? AND date <= ? ORDER BY date`, uid, metric, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyPoint{}
	for rows.Next() {
		var p DailyPoint
		var v2, lo, hi sql.NullFloat64
		if err := rows.Scan(&p.Date, &p.Value, &v2, &lo, &hi, &p.N); err != nil {
			return nil, err
		}
		p.Value2, p.Min, p.Max = nf(v2), nf(lo), nf(hi)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DailyMany returns several metrics' days in a range.
func (s *Store) DailyMany(ctx context.Context, uid int64, keys []string, from, to string) (map[string][]DailyPoint, error) {
	out := map[string][]DailyPoint{}
	for _, k := range keys {
		pts, err := s.Daily(ctx, uid, k, from, to)
		if err != nil {
			return nil, err
		}
		out[k] = pts
	}
	return out, nil
}

// DayValues returns every metric's number on one day.
func (s *Store) DayValues(ctx context.Context, uid int64, date string) (map[string]DailyPoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT metric, date, value, value2, min, max, n FROM daily WHERE user_id = ? AND date = ?`, uid, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DailyPoint{}
	for rows.Next() {
		var k string
		var p DailyPoint
		var v2, lo, hi sql.NullFloat64
		if err := rows.Scan(&k, &p.Date, &p.Value, &v2, &lo, &hi, &p.N); err != nil {
			return nil, err
		}
		p.Value2, p.Min, p.Max = nf(v2), nf(lo), nf(hi)
		out[k] = p
	}
	return out, rows.Err()
}

// MetricStat is what a person has of one metric.
type MetricStat struct {
	Metric  string   `json:"metric"`
	Days    int      `json:"days"`
	First   string   `json:"first"`
	Last    string   `json:"last"`
	Latest  float64  `json:"latest"`
	Latest2 *float64 `json:"latest2,omitempty"`
}

// MetricStats lists the metrics a person has data for.
func (s *Store) MetricStats(ctx context.Context, uid int64) ([]MetricStat, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT metric, COUNT(1), MIN(date), MAX(date) FROM daily WHERE user_id = ? GROUP BY metric`, uid)
	if err != nil {
		return nil, err
	}
	var out []MetricStat
	for rows.Next() {
		var m MetricStat
		if err := rows.Scan(&m.Metric, &m.Days, &m.First, &m.Last); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		var v sql.NullFloat64
		var v2 sql.NullFloat64
		_ = s.DB.QueryRowContext(ctx, `SELECT value, value2 FROM daily WHERE user_id = ? AND metric = ? AND date = ?`, uid, out[i].Metric, out[i].Last).Scan(&v, &v2)
		out[i].Latest = v.Float64
		out[i].Latest2 = nf(v2)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	if out == nil {
		out = []MetricStat{}
	}
	return out, nil
}

// LatestDaily is the last day with a value for a metric.
func (s *Store) LatestDaily(ctx context.Context, uid int64, metric string) (*DailyPoint, error) {
	var p DailyPoint
	var v2, lo, hi sql.NullFloat64
	err := s.DB.QueryRowContext(ctx, `SELECT date, value, value2, min, max, n FROM daily WHERE user_id = ? AND metric = ? ORDER BY date DESC LIMIT 1`, uid, metric).Scan(&p.Date, &p.Value, &v2, &lo, &hi, &p.N)
	if err != nil {
		return nil, notFound(err)
	}
	p.Value2, p.Min, p.Max = nf(v2), nf(lo), nf(hi)
	return &p, nil
}

// SampleFilter narrows a samples listing.
type SampleFilter struct {
	Metric string
	From   string
	To     string
	Source string
	Limit  int
	Desc   bool
}

// Samples lists raw samples.
func (s *Store) Samples(ctx context.Context, uid int64, f SampleFilter) ([]Sample, error) {
	q := `SELECT ` + sampleCols + ` FROM samples WHERE user_id = ?`
	args := []any{uid}
	if f.Metric != "" {
		q += ` AND metric = ?`
		args = append(args, f.Metric)
	}
	if f.From != "" {
		q += ` AND date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND date <= ?`
		args = append(args, f.To)
	}
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, f.Source)
	}
	if f.Desc {
		q += ` ORDER BY start_at DESC, id DESC`
	} else {
		q += ` ORDER BY start_at, id`
	}
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sample{}
	for rows.Next() {
		sm, err := scanSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// GetSample returns one of the person's samples.
func (s *Store) GetSample(ctx context.Context, uid, id int64) (*Sample, error) {
	sm, err := scanSample(s.DB.QueryRowContext(ctx, `SELECT `+sampleCols+` FROM samples WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return &sm, nil
}

// UpdateSample changes a sample's value, note or time (manual entries).
func (s *Store) UpdateSample(ctx context.Context, uid int64, sm *Sample) (Touched, error) {
	old, err := s.GetSample(ctx, uid, sm.ID)
	if err != nil {
		return nil, err
	}
	sm.Date = DateOf(sm.StartAt)
	_, err = s.DB.ExecContext(ctx, `UPDATE samples SET start_at = ?, end_at = ?, date = ?, value = ?, value2 = ?, note = ? WHERE id = ? AND user_id = ?`,
		sm.StartAt, sm.EndAt, sm.Date, sm.Value, pf(sm.Value2), sm.Note, sm.ID, uid)
	if err != nil {
		return nil, conflict(err)
	}
	t := Touched{}
	t.add(old.Metric, old.Date)
	t.add(old.Metric, sm.Date)
	return t, nil
}

// DeleteSample removes a sample and says which (metric, date) to roll up.
func (s *Store) DeleteSample(ctx context.Context, uid, id int64) (Touched, error) {
	sm, err := s.GetSample(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM samples WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return nil, err
	}
	t := Touched{}
	t.add(sm.Metric, sm.Date)
	return t, nil
}

// DeleteSource removes everything a source wrote (undo an import).
func (s *Store) DeleteSource(ctx context.Context, uid int64, source string) (int64, error) {
	var n int64
	res, err := s.DB.ExecContext(ctx, `DELETE FROM samples WHERE user_id = ? AND source = ?`, uid, source)
	if err != nil {
		return 0, err
	}
	n, _ = res.RowsAffected()
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM workouts WHERE user_id = ? AND source = ?`, uid, source); err != nil {
		return n, err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sleep_sessions WHERE user_id = ? AND source = ?`, uid, source); err != nil {
		return n, err
	}
	return n, nil
}

// FirstDate is the earliest day a person has anything.
func (s *Store) FirstDate(ctx context.Context, uid int64) string {
	var d sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(d) FROM (SELECT MIN(date) AS d FROM daily WHERE user_id = ? UNION ALL SELECT MIN(date) FROM workouts WHERE user_id = ? UNION ALL SELECT MIN(date) FROM sleep_sessions WHERE user_id = ?)`, uid, uid, uid).Scan(&d)
	return d.String
}

// LastSync is the newest sample time per source (the Sources page).
func (s *Store) LastSync(ctx context.Context, uid int64) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT source, MAX(created_at) FROM samples WHERE user_id = ? GROUP BY source`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var src, at string
		if err := rows.Scan(&src, &at); err != nil {
			return nil, err
		}
		out[src] = at
	}
	return out, rows.Err()
}

// Devices lists the device names seen (for the "prefer device" setting).
func (s *Store) Devices(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT device FROM samples WHERE user_id = ? AND device <> '' ORDER BY device`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// WeeklyAverages averages a metric per ISO week (for trends); weeks keyed by
// their Monday.
func WeeklyAverages(pts []DailyPoint, sum bool) []DailyPoint {
	byWeek := map[string][]float64{}
	var keys []string
	for _, p := range pts {
		t, err := ParseDate(p.Date)
		if err != nil {
			continue
		}
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		mon := t.AddDate(0, 0, -(wd - 1)).Format("2006-01-02")
		if _, ok := byWeek[mon]; !ok {
			keys = append(keys, mon)
		}
		byWeek[mon] = append(byWeek[mon], p.Value)
	}
	sort.Strings(keys)
	out := make([]DailyPoint, 0, len(keys))
	for _, k := range keys {
		vs := byWeek[k]
		t := 0.0
		for _, v := range vs {
			t += v
		}
		if !sum {
			t /= float64(len(vs))
		}
		out = append(out, DailyPoint{Date: k, Value: t, N: len(vs)})
	}
	return out
}
