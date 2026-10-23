CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- One measurement: a metric's value at a moment (or over a span), in the
-- metric's canonical unit. Everything is per person. A sample is unique per
-- metric, start time, source and device, so a phone re-sending a window
-- (today's steps growing through the day) updates instead of doubling.
CREATE TABLE samples (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  metric     TEXT NOT NULL,
  start_at   TEXT NOT NULL,                    -- RFC3339 with offset
  end_at     TEXT NOT NULL DEFAULT '',
  date       TEXT NOT NULL,                    -- local date of start_at
  value      REAL NOT NULL,
  value2     REAL,                             -- pair metrics (diastolic)
  value_min  REAL,                             -- an aggregated input's range (heart rate min/max)
  value_max  REAL,
  unit       TEXT NOT NULL DEFAULT '',
  source     TEXT NOT NULL DEFAULT 'manual',   -- manual | hae | apple | api | <pocket app slug>
  device     TEXT NOT NULL DEFAULT '',         -- Apple Watch, iPhone, Withings…
  note       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX samples_key ON samples(user_id, metric, start_at, source, device);
CREATE INDEX samples_user_metric_date ON samples(user_id, metric, date);
CREATE INDEX samples_user_date ON samples(user_id, date);

-- A day's number per metric, rolled up from the samples with the metric's
-- rule (sum, average, last…) and recomputed whenever a day's samples change.
CREATE TABLE daily (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  metric     TEXT NOT NULL,
  date       TEXT NOT NULL,
  value      REAL NOT NULL,
  value2     REAL,
  min        REAL,
  max        REAL,
  n          INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, metric, date)
);
CREATE INDEX daily_user_date ON daily(user_id, date);

CREATE TABLE workouts (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL DEFAULT 'other',   -- running | walking | cycling | strength | …
  name            TEXT NOT NULL DEFAULT '',
  start_at        TEXT NOT NULL,
  end_at          TEXT NOT NULL DEFAULT '',
  date            TEXT NOT NULL,
  duration_s      INTEGER NOT NULL DEFAULT 0,
  distance_m      REAL NOT NULL DEFAULT 0,
  energy_kcal     REAL NOT NULL DEFAULT 0,
  avg_hr          REAL NOT NULL DEFAULT 0,
  max_hr          REAL NOT NULL DEFAULT 0,
  elevation_m     REAL NOT NULL DEFAULT 0,
  steps           INTEGER NOT NULL DEFAULT 0,
  temperature     REAL,
  humidity        REAL,
  source          TEXT NOT NULL DEFAULT 'manual',
  device          TEXT NOT NULL DEFAULT '',
  external_id     TEXT NOT NULL DEFAULT '',
  notes           TEXT NOT NULL DEFAULT '',
  habit_id        INTEGER,                        -- Pocket Habits habit ticked for this workout
  habit_marked_at TEXT NOT NULL DEFAULT '',
  habit_error     TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX workouts_key ON workouts(user_id, start_at, kind, source);
CREATE INDEX workouts_user_date ON workouts(user_id, date);

-- One night. The date is the morning you woke up.
CREATE TABLE sleep_sessions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_at   TEXT NOT NULL,
  end_at     TEXT NOT NULL,
  date       TEXT NOT NULL,
  in_bed_s   INTEGER NOT NULL DEFAULT 0,
  asleep_s   INTEGER NOT NULL DEFAULT 0,
  core_s     INTEGER NOT NULL DEFAULT 0,
  deep_s     INTEGER NOT NULL DEFAULT 0,
  rem_s      INTEGER NOT NULL DEFAULT 0,
  awake_s    INTEGER NOT NULL DEFAULT 0,
  source     TEXT NOT NULL DEFAULT 'manual',
  device     TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX sleep_key ON sleep_sessions(user_id, start_at, source);
CREATE INDEX sleep_user_date ON sleep_sessions(user_id, date);

-- A target per metric: at least 8000 steps a day, at most 80 kg…
CREATE TABLE goals (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  metric     TEXT NOT NULL,
  target     REAL NOT NULL,
  direction  TEXT NOT NULL DEFAULT 'at_least',  -- at_least | at_most
  period     TEXT NOT NULL DEFAULT 'day',       -- day | week
  active     INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX goals_metric ON goals(user_id, metric);

-- Days a daily goal was met (streaks, one push per day).
CREATE TABLE goal_hits (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  metric   TEXT NOT NULL,
  date     TEXT NOT NULL,
  value    REAL NOT NULL DEFAULT 0,
  notified INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, metric, date)
);

CREATE TABLE medications (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  dose       TEXT NOT NULL DEFAULT '',          -- "500 mg", "1 tablet"
  form       TEXT NOT NULL DEFAULT '',          -- tablet | capsule | drops | injection | …
  times      TEXT NOT NULL DEFAULT '08:00',     -- "08:00,20:00"
  days       TEXT NOT NULL DEFAULT '1234567',   -- ISO weekdays
  notes      TEXT NOT NULL DEFAULT '',
  active     INTEGER NOT NULL DEFAULT 1,
  start_date TEXT NOT NULL DEFAULT '',
  end_date   TEXT NOT NULL DEFAULT '',
  remind     INTEGER NOT NULL DEFAULT 1,
  stock      INTEGER NOT NULL DEFAULT -1,       -- doses left, -1 = not tracked
  color      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX medications_user ON medications(user_id, active);

CREATE TABLE med_log (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  medication_id INTEGER NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
  date          TEXT NOT NULL,
  slot          TEXT NOT NULL DEFAULT '',       -- the scheduled time, '' for an extra dose
  taken_at      TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'taken',  -- taken | skipped
  note          TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX med_log_slot ON med_log(user_id, medication_id, date, slot) WHERE slot <> '';
CREATE INDEX med_log_user_date ON med_log(user_id, date);

-- Reminders already sent (medication, date, slot) so a dose is nagged once.
CREATE TABLE med_reminders (
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  medication_id INTEGER NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
  date          TEXT NOT NULL,
  slot          TEXT NOT NULL,
  sent_at       TEXT NOT NULL,
  PRIMARY KEY (user_id, medication_id, date, slot)
);

CREATE TABLE symptoms (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  severity     INTEGER NOT NULL DEFAULT 2,      -- 1 mild … 5 severe
  at           TEXT NOT NULL,
  date         TEXT NOT NULL,
  duration_min INTEGER NOT NULL DEFAULT 0,
  note         TEXT NOT NULL DEFAULT '',
  tags         TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX symptoms_user_date ON symptoms(user_id, date);

-- "A run ticks my Gym habit": what in Health marks which Pocket Habits habit.
CREATE TABLE habit_rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trigger    TEXT NOT NULL DEFAULT 'workout',   -- workout | goal
  match      TEXT NOT NULL DEFAULT 'any',       -- a workout kind / 'any', or a metric key
  habit_id   INTEGER NOT NULL,
  habit_name TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX habit_rules_user ON habit_rules(user_id);

-- Days a goal rule already ticked a habit.
CREATE TABLE habit_marks (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  rule_id  INTEGER NOT NULL REFERENCES habit_rules(id) ON DELETE CASCADE,
  date     TEXT NOT NULL,
  marked_at TEXT NOT NULL,
  PRIMARY KEY (user_id, rule_id, date)
);

-- The day's weather at the home location (Open-Meteo), for the insights.
CREATE TABLE weather_days (
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  date         TEXT NOT NULL,
  code         INTEGER NOT NULL DEFAULT -1,
  condition    TEXT NOT NULL DEFAULT '',
  t_max        REAL,
  t_min        REAL,
  precip_mm    REAL,
  sunshine_h   REAL,
  daylight_h   REAL,
  lat          REAL NOT NULL DEFAULT 0,
  lon          REAL NOT NULL DEFAULT 0,
  place        TEXT NOT NULL DEFAULT '',
  fetched_at   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, date)
);

-- Background imports (an Apple Health export can take minutes).
CREATE TABLE imports (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,                    -- apple | hae | csv | json
  file        TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'running',  -- running | done | error
  progress    TEXT NOT NULL DEFAULT '',
  samples     INTEGER NOT NULL DEFAULT 0,
  workouts    INTEGER NOT NULL DEFAULT 0,
  sleep       INTEGER NOT NULL DEFAULT 0,
  skipped     INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  started_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  finished_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX imports_user ON imports(user_id, id);
