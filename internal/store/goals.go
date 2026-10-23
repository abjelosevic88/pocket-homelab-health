package store

import (
	"context"
	"database/sql"
)

// ListGoals returns a person's goals.
func (s *Store) ListGoals(ctx context.Context, uid int64) ([]Goal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, metric, target, direction, period, active, created_at FROM goals WHERE user_id = ? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Goal{}
	for rows.Next() {
		var g Goal
		var active int
		if err := rows.Scan(&g.ID, &g.UserID, &g.Metric, &g.Target, &g.Direction, &g.Period, &active, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.Active = active == 1
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetGoal returns one goal by metric.
func (s *Store) GetGoal(ctx context.Context, uid int64, metric string) (*Goal, error) {
	var g Goal
	var active int
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, metric, target, direction, period, active, created_at FROM goals WHERE user_id = ? AND metric = ?`, uid, metric).Scan(&g.ID, &g.UserID, &g.Metric, &g.Target, &g.Direction, &g.Period, &active, &g.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	g.Active = active == 1
	return &g, nil
}

// UpsertGoal saves a goal (one per metric).
func (s *Store) UpsertGoal(ctx context.Context, uid int64, g *Goal) error {
	if g.Direction != "at_most" {
		g.Direction = "at_least"
	}
	if g.Period != "week" {
		g.Period = "day"
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO goals (user_id, metric, target, direction, period, active) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, metric) DO UPDATE SET target = excluded.target, direction = excluded.direction, period = excluded.period, active = excluded.active`,
		uid, g.Metric, g.Target, g.Direction, g.Period, boolInt(g.Active))
	if err != nil {
		return err
	}
	got, err := s.GetGoal(ctx, uid, g.Metric)
	if err != nil {
		return err
	}
	*g = *got
	return nil
}

// DeleteGoal removes a goal and its hits.
func (s *Store) DeleteGoal(ctx context.Context, uid int64, metric string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM goals WHERE user_id = ? AND metric = ?`, uid, metric)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM goal_hits WHERE user_id = ? AND metric = ?`, uid, metric)
	return err
}

// RecordGoalHit marks a day as met; true when it is new.
func (s *Store) RecordGoalHit(ctx context.Context, uid int64, metric, date string, value float64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO goal_hits (user_id, metric, date, value) VALUES (?, ?, ?, ?) ON CONFLICT(user_id, metric, date) DO UPDATE SET value = excluded.value`, uid, metric, date, value)
	if err != nil {
		return false, err
	}
	var notified int
	_ = s.DB.QueryRowContext(ctx, `SELECT notified FROM goal_hits WHERE user_id = ? AND metric = ? AND date = ?`, uid, metric, date).Scan(&notified)
	_ = res
	return notified == 0, nil
}

// ClearGoalHit removes a day that no longer meets the goal (a sample was deleted).
func (s *Store) ClearGoalHit(ctx context.Context, uid int64, metric, date string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM goal_hits WHERE user_id = ? AND metric = ? AND date = ?`, uid, metric, date)
	return err
}

// MarkGoalNotified records that the push went out.
func (s *Store) MarkGoalNotified(ctx context.Context, uid int64, metric, date string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE goal_hits SET notified = 1 WHERE user_id = ? AND metric = ? AND date = ?`, uid, metric, date)
	return err
}

// GoalHits lists the days a goal was met in a range.
func (s *Store) GoalHits(ctx context.Context, uid int64, metric, from, to string) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date FROM goal_hits WHERE user_id = ? AND metric = ? AND date >= ? AND date <= ?`, uid, metric, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// GoalStreak counts consecutive met days ending today or yesterday.
func (s *Store) GoalStreak(ctx context.Context, uid int64, metric string) (int, int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date FROM goal_hits WHERE user_id = ? AND metric = ? ORDER BY date DESC LIMIT 400`, uid, metric)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return 0, 0, err
		}
		dates = append(dates, d)
	}
	if len(dates) == 0 {
		return 0, 0, nil
	}
	today := Today()
	cur := 0
	expect := today
	if dates[0] != today {
		expect = AddDays(today, -1)
	}
	for _, d := range dates {
		if d != expect {
			break
		}
		cur++
		expect = AddDays(expect, -1)
	}
	best, run := 0, 0
	for i := len(dates) - 1; i >= 0; i-- {
		if i == len(dates)-1 || AddDays(dates[i+1], 1) == dates[i] {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	return cur, best, nil
}

// ---- habit rules ----

const ruleCols = `id, user_id, trigger, match, habit_id, habit_name, enabled, created_at`

func scanRule(row interface{ Scan(...any) error }) (HabitRule, error) {
	var r HabitRule
	var en int
	if err := row.Scan(&r.ID, &r.UserID, &r.Trigger, &r.Match, &r.HabitID, &r.HabitName, &en, &r.CreatedAt); err != nil {
		return r, notFound(err)
	}
	r.Enabled = en == 1
	return r, nil
}

// ListRules lists a person's habit rules.
func (s *Store) ListRules(ctx context.Context, uid int64) ([]HabitRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+ruleCols+` FROM habit_rules WHERE user_id = ? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HabitRule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRule adds one.
func (s *Store) CreateRule(ctx context.Context, uid int64, r *HabitRule) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO habit_rules (user_id, trigger, match, habit_id, habit_name, enabled) VALUES (?, ?, ?, ?, ?, ?)`, uid, r.Trigger, r.Match, r.HabitID, r.HabitName, boolInt(r.Enabled))
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	r.UserID = uid
	return nil
}

// UpdateRule saves edits.
func (s *Store) UpdateRule(ctx context.Context, uid int64, r *HabitRule) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE habit_rules SET trigger = ?, match = ?, habit_id = ?, habit_name = ?, enabled = ? WHERE id = ? AND user_id = ?`, r.Trigger, r.Match, r.HabitID, r.HabitName, boolInt(r.Enabled), r.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRule removes one.
func (s *Store) DeleteRule(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM habit_rules WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RuleMarked reports whether a goal rule already ticked its habit on a day.
func (s *Store) RuleMarked(ctx context.Context, uid, ruleID int64, date string) bool {
	var at string
	err := s.DB.QueryRowContext(ctx, `SELECT marked_at FROM habit_marks WHERE user_id = ? AND rule_id = ? AND date = ?`, uid, ruleID, date).Scan(&at)
	return err != sql.ErrNoRows && err == nil
}

// SetRuleMarked records the tick.
func (s *Store) SetRuleMarked(ctx context.Context, uid, ruleID int64, date string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO habit_marks (user_id, rule_id, date, marked_at) VALUES (?, ?, ?, ?)`, uid, ruleID, date, Now())
	return err
}
