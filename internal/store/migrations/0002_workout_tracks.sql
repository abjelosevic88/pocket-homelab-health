-- What the phone recorded during a workout, beyond its totals: total energy
-- (active + resting), the town it started in, and the GPS route and heart rate
-- over time (Apple Health's workout route and heart-rate samples).
ALTER TABLE workouts ADD COLUMN total_kcal REAL NOT NULL DEFAULT 0;
ALTER TABLE workouts ADD COLUMN place TEXT NOT NULL DEFAULT '';

-- route: JSON [[lat, lng, altitude_m, seconds_from_start], …]
-- heart_rate: JSON [[seconds_from_start, bpm], …]
CREATE TABLE workout_tracks (
  workout_id  INTEGER PRIMARY KEY REFERENCES workouts(id) ON DELETE CASCADE,
  route       TEXT NOT NULL DEFAULT '[]',
  heart_rate  TEXT NOT NULL DEFAULT '[]',
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
