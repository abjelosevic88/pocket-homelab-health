package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- Pocket Habits: rules that tick a habit ----

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRules(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "rules")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type ruleInput struct {
	Trigger   string `json:"trigger"`
	Match     string `json:"match"`
	HabitID   int64  `json:"habit_id"`
	HabitName string `json:"habit_name"`
	Enabled   *bool  `json:"enabled"`
}

func (in *ruleInput) apply(ru *store.HabitRule) error {
	if in.Trigger != "" {
		ru.Trigger = in.Trigger
	}
	if ru.Trigger != "workout" && ru.Trigger != "goal" {
		return fmt.Errorf("trigger must be workout or goal")
	}
	if in.Match != "" {
		ru.Match = strings.ToLower(strings.TrimSpace(in.Match))
	}
	if ru.Match == "" {
		ru.Match = "any"
	}
	if ru.Trigger == "goal" && !metrics.Known(ru.Match) {
		return fmt.Errorf("a goal rule needs a metric")
	}
	if in.HabitID > 0 {
		ru.HabitID = in.HabitID
	}
	if ru.HabitID <= 0 {
		return fmt.Errorf("pick a habit")
	}
	if in.HabitName != "" {
		ru.HabitName = strings.TrimSpace(in.HabitName)
	}
	if in.Enabled != nil {
		ru.Enabled = *in.Enabled
	}
	return nil
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ru := store.HabitRule{Trigger: "workout", Match: "any", Enabled: true}
	if err := in.apply(&ru); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateRule(r.Context(), actorID(r.Context()), &ru); err != nil {
		s.fail(w, err, "create rule")
		return
	}
	writeJSON(w, http.StatusCreated, ru)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	rules, err := s.store.ListRules(ctx, uid)
	if err != nil {
		s.fail(w, err, "rules")
		return
	}
	var ru *store.HabitRule
	for i := range rules {
		if rules[i].ID == idParam(r, "id") {
			ru = &rules[i]
		}
	}
	if ru == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in ruleInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := in.apply(ru); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateRule(ctx, uid, ru); err != nil {
		s.fail(w, err, "update rule")
		return
	}
	writeJSON(w, http.StatusOK, ru)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRule(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleHabitsList lists the person's habits from Pocket Habits (through
// the connection in effect) for the rule picker.
func (s *Server) handleHabitsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	conn, err := s.clients(ctx, uid).conn(ctx, appHabits, scopeHabitsRead)
	if isNotConnected(err) {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "detail": err.Error(), "habits": []any{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "detail": err.Error(), "habits": []any{}})
		return
	}
	habits, err := conn.client.Habits(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "detail": err.Error(), "habits": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "via": conn.via, "habits": habits, "open_url": conn.public})
}

// handleMarkNow runs the pending workout ticks for the caller right away.
func (s *Server) handleMarkNow(w http.ResponseWriter, r *http.Request) {
	n := s.markPendingWorkouts(r.Context(), actorID(r.Context()))
	writeJSON(w, http.StatusOK, map[string]any{"marked": n})
}

// tickHabit marks a habit done on a date in Pocket Habits as the person.
func (s *Server) tickHabit(ctx context.Context, uid, habitID int64, date string) error {
	conn, err := s.clients(ctx, uid).conn(ctx, appHabits, scopeEntriesWrite)
	if err != nil {
		return err
	}
	return conn.client.MarkHabit(ctx, habitID, date)
}

// markPendingWorkouts applies the workout rules to recent workouts that
// have not ticked a habit yet (the last 7 days). Returns how many ticked.
func (s *Server) markPendingWorkouts(ctx context.Context, uid int64) int {
	s.ruleMu.Lock()
	defer s.ruleMu.Unlock()
	rules, err := s.store.ListRules(ctx, uid)
	if err != nil || len(rules) == 0 {
		return 0
	}
	var active []store.HabitRule
	for _, ru := range rules {
		if ru.Enabled && ru.Trigger == "workout" {
			active = append(active, ru)
		}
	}
	if len(active) == 0 {
		return 0
	}
	pending, err := s.store.WorkoutsToMark(ctx, uid, store.AddDays(store.Today(), -7))
	if err != nil {
		return 0
	}
	n := 0
	for _, wk := range pending {
		var matched *store.HabitRule
		for i := range active {
			if active[i].Match == "any" || active[i].Match == wk.Kind {
				matched = &active[i]
				break
			}
		}
		if matched == nil {
			_ = s.store.SetWorkoutHabit(ctx, uid, wk.ID, 0, "") // nothing to tick: done
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.tickHabit(cctx, uid, matched.HabitID, wk.Date)
		cancel()
		if err != nil {
			if isNotConnected(err) {
				_ = s.store.SetWorkoutHabit(ctx, uid, wk.ID, 0, "") // no connection: nothing to do, do not retry forever
			} else {
				_ = s.store.SetWorkoutHabit(ctx, uid, wk.ID, 0, err.Error())
				s.log.Warn("habit tick failed", "user", uid, "workout", wk.ID, "err", err)
			}
			continue
		}
		_ = s.store.SetWorkoutHabit(ctx, uid, wk.ID, matched.HabitID, "")
		n++
	}
	return n
}

// runGoalRules ticks the habits of goal rules for a metric met on a date.
func (s *Server) runGoalRules(ctx context.Context, uid int64, metric, date string) {
	rules, err := s.store.ListRules(ctx, uid)
	if err != nil {
		return
	}
	for _, ru := range rules {
		if !ru.Enabled || ru.Trigger != "goal" || ru.Match != metric || s.store.RuleMarked(ctx, uid, ru.ID, date) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.tickHabit(cctx, uid, ru.HabitID, date)
		cancel()
		if err != nil {
			if !isNotConnected(err) {
				s.log.Warn("goal habit tick failed", "user", uid, "rule", ru.ID, "err", err)
			}
			continue
		}
		_ = s.store.SetRuleMarked(ctx, uid, ru.ID, date)
	}
}
