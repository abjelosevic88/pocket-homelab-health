-- The planned (WorkoutKit custom) workout a session ran: its id and display
-- name, so a session can be matched with its plan.
ALTER TABLE workouts ADD COLUMN plan_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workouts ADD COLUMN plan_name TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_workouts_plan ON workouts(user_id, plan_id);
-- The spacing of the series as they were sent: 0 = the device's own (native)
-- rate, else seconds (older phones sent every 5 s).
ALTER TABLE workout_tracks ADD COLUMN series_step REAL NOT NULL DEFAULT 5;
