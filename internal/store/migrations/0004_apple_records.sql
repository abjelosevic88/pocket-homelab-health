-- What Apple Health keeps besides numbers: symptoms (into the symptom log,
-- updated in place when the phone sends them again), ECG recordings and
-- hearing tests.
ALTER TABLE symptoms ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE symptoms ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX symptoms_external ON symptoms(user_id, external_id) WHERE external_id <> '';

-- One ECG: Apple's classification, the heart rate, and the voltage trace
-- (JSON array of µV at sampling_hz).
CREATE TABLE ecgs (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at              TEXT NOT NULL,
  date            TEXT NOT NULL,
  duration_s      REAL NOT NULL DEFAULT 0,
  classification  TEXT NOT NULL DEFAULT '',
  avg_hr          REAL NOT NULL DEFAULT 0,
  symptoms_status TEXT NOT NULL DEFAULT '',
  sampling_hz     REAL NOT NULL DEFAULT 0,
  voltages        TEXT NOT NULL DEFAULT '[]',
  source          TEXT NOT NULL DEFAULT '',
  device          TEXT NOT NULL DEFAULT '',
  external_id     TEXT NOT NULL,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX ecgs_external ON ecgs(user_id, external_id);
CREATE INDEX ecgs_user_date ON ecgs(user_id, date);

-- One hearing test: [[frequency Hz, left dB HL or null, right dB HL or null], …].
CREATE TABLE audiograms (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at          TEXT NOT NULL,
  date        TEXT NOT NULL,
  points      TEXT NOT NULL DEFAULT '[]',
  source      TEXT NOT NULL DEFAULT '',
  device      TEXT NOT NULL DEFAULT '',
  external_id TEXT NOT NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX audiograms_external ON audiograms(user_id, external_id);
