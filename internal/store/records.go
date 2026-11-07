package store

import (
	"context"
	"encoding/json"
)

// ECG is one electrocardiogram recorded by a watch.
type ECG struct {
	ID             int64   `json:"id"`
	At             string  `json:"at"`
	Date           string  `json:"date"`
	DurationS      float64 `json:"duration_s"`
	Classification string  `json:"classification"` // sinus_rhythm | atrial_fibrillation | inconclusive_… | unrecognized
	AvgHR          float64 `json:"avg_hr"`
	SymptomsStatus string  `json:"symptoms_status"` // none | present | ""
	SamplingHz     float64 `json:"sampling_hz"`
	// µV at SamplingHz; left out of listings.
	Voltages   []float64 `json:"voltages,omitempty"`
	Source     string    `json:"source"`
	Device     string    `json:"device"`
	ExternalID string    `json:"external_id"`
}

// Audiogram is one hearing test: per frequency the threshold of each ear.
type Audiogram struct {
	ID         int64         `json:"id"`
	At         string        `json:"at"`
	Date       string        `json:"date"`
	Points     [][3]*float64 `json:"points"` // [Hz, left dB HL, right dB HL]
	Source     string        `json:"source"`
	Device     string        `json:"device"`
	ExternalID string        `json:"external_id"`
}

// UpsertECGs keeps ECGs by their id on the device.
func (s *Store) UpsertECGs(ctx context.Context, uid int64, in []ECG) (int, error) {
	n := 0
	for _, e := range in {
		if e.At == "" || e.ExternalID == "" {
			continue
		}
		v, _ := json.Marshal(e.Voltages)
		if e.Voltages == nil {
			v = []byte("[]")
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO ecgs (user_id, at, date, duration_s, classification, avg_hr, symptoms_status, sampling_hz, voltages, source, device, external_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, external_id) DO UPDATE SET at = excluded.at, date = excluded.date, duration_s = excluded.duration_s, classification = excluded.classification,
				avg_hr = excluded.avg_hr, symptoms_status = excluded.symptoms_status, sampling_hz = excluded.sampling_hz,
				voltages = CASE WHEN excluded.voltages <> '[]' THEN excluded.voltages ELSE voltages END, source = excluded.source, device = excluded.device`,
			uid, e.At, DateOf(e.At), e.DurationS, e.Classification, e.AvgHR, e.SymptomsStatus, e.SamplingHz, string(v), e.Source, e.Device, e.ExternalID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ListECGs lists ECGs newest first, without their traces.
func (s *Store) ListECGs(ctx context.Context, uid int64, limit int) ([]ECG, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, at, date, duration_s, classification, avg_hr, symptoms_status, sampling_hz, source, device, external_id FROM ecgs WHERE user_id = ? ORDER BY at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ECG{}
	for rows.Next() {
		var e ECG
		if err := rows.Scan(&e.ID, &e.At, &e.Date, &e.DurationS, &e.Classification, &e.AvgHR, &e.SymptomsStatus, &e.SamplingHz, &e.Source, &e.Device, &e.ExternalID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetECG returns one ECG with its trace.
func (s *Store) GetECG(ctx context.Context, uid, id int64) (*ECG, error) {
	var e ECG
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT id, at, date, duration_s, classification, avg_hr, symptoms_status, sampling_hz, voltages, source, device, external_id FROM ecgs WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&e.ID, &e.At, &e.Date, &e.DurationS, &e.Classification, &e.AvgHR, &e.SymptomsStatus, &e.SamplingHz, &v, &e.Source, &e.Device, &e.ExternalID)
	if err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(v), &e.Voltages)
	return &e, nil
}

// UpsertAudiograms keeps hearing tests by their id on the device.
func (s *Store) UpsertAudiograms(ctx context.Context, uid int64, in []Audiogram) (int, error) {
	n := 0
	for _, a := range in {
		if a.At == "" || a.ExternalID == "" {
			continue
		}
		p, _ := json.Marshal(a.Points)
		if a.Points == nil {
			p = []byte("[]")
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO audiograms (user_id, at, date, points, source, device, external_id) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, external_id) DO UPDATE SET at = excluded.at, date = excluded.date, points = excluded.points, source = excluded.source, device = excluded.device`,
			uid, a.At, DateOf(a.At), string(p), a.Source, a.Device, a.ExternalID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ListAudiograms lists hearing tests newest first.
func (s *Store) ListAudiograms(ctx context.Context, uid int64, limit int) ([]Audiogram, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, at, date, points, source, device, external_id FROM audiograms WHERE user_id = ? ORDER BY at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Audiogram{}
	for rows.Next() {
		var a Audiogram
		var p string
		if err := rows.Scan(&a.ID, &a.At, &a.Date, &p, &a.Source, &a.Device, &a.ExternalID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(p), &a.Points)
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertSymptomsFrom keeps symptoms another app recorded, by their id there:
// sent again they update in place, never doubling. Ones typed in here are
// left alone.
func (s *Store) UpsertSymptomsFrom(ctx context.Context, uid int64, source string, in []Symptom) (int, error) {
	n := 0
	for _, x := range in {
		if x.At == "" || x.Name == "" || x.ExternalID == "" {
			continue
		}
		x.Severity = max(1, min(5, x.Severity))
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO symptoms (user_id, name, severity, at, date, duration_min, note, tags, source, external_id) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?)
			ON CONFLICT(user_id, external_id) WHERE external_id <> '' DO UPDATE SET name = excluded.name, severity = excluded.severity, at = excluded.at, date = excluded.date,
				duration_min = excluded.duration_min, note = excluded.note, source = excluded.source`,
			uid, x.Name, x.Severity, x.At, DateOf(x.At), x.DurationMin, x.Note, source, x.ExternalID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
