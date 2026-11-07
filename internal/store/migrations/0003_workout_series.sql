-- A workout's own detail: every quantity the watch or phone recorded along it
-- (heart rate, speed, power, cadence, distance…) as {"key": [[seconds, value], …]},
-- its laps, segments, intervals and activities, and the effort rating.
ALTER TABLE workout_tracks ADD COLUMN series TEXT NOT NULL DEFAULT '{}';
ALTER TABLE workout_tracks ADD COLUMN segments TEXT NOT NULL DEFAULT '[]';
UPDATE workout_tracks SET series = json_object('heart_rate', json(heart_rate)) WHERE heart_rate NOT IN ('[]', 'null', '');
ALTER TABLE workout_tracks DROP COLUMN heart_rate;
ALTER TABLE workouts ADD COLUMN effort REAL;
ALTER TABLE workouts ADD COLUMN effort_estimated REAL;
