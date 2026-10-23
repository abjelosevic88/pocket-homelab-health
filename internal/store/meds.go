package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const medCols = `id, user_id, name, dose, form, times, days, notes, active, start_date, end_date, remind, stock, color, created_at, updated_at`

func scanMed(row interface{ Scan(...any) error }) (Medication, error) {
	var m Medication
	var active, remind int
	if err := row.Scan(&m.ID, &m.UserID, &m.Name, &m.Dose, &m.Form, &m.Times, &m.Days, &m.Notes, &active, &m.StartDate, &m.EndDate, &remind, &m.Stock, &m.Color, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return m, notFound(err)
	}
	m.Active, m.Remind, m.Adherence = active == 1, remind == 1, -1
	return m, nil
}

// NormalizeTimes cleans "8:00, 20:00" into "08:00,20:00".
func NormalizeTimes(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		t, err := time.Parse("15:04", p)
		if err != nil {
			if t2, err2 := time.Parse("3:04", p); err2 == nil {
				t = t2
			} else {
				continue
			}
		}
		k := t.Format("15:04")
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// ListMedications lists a person's medications (active first).
func (s *Store) ListMedications(ctx context.Context, uid int64, all bool) ([]Medication, error) {
	q := `SELECT ` + medCols + ` FROM medications WHERE user_id = ?`
	if !all {
		q += ` AND active = 1`
	}
	q += ` ORDER BY active DESC, name`
	rows, err := s.DB.QueryContext(ctx, q, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Medication{}
	for rows.Next() {
		m, err := scanMed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMedication returns one.
func (s *Store) GetMedication(ctx context.Context, uid, id int64) (*Medication, error) {
	m, err := scanMed(s.DB.QueryRowContext(ctx, `SELECT `+medCols+` FROM medications WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMedication adds one.
func (s *Store) CreateMedication(ctx context.Context, uid int64, m *Medication) error {
	m.Times = NormalizeTimes(m.Times)
	if m.Days == "" {
		m.Days = "1234567"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO medications (user_id, name, dose, form, times, days, notes, active, start_date, end_date, remind, stock, color) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, m.Name, m.Dose, m.Form, m.Times, m.Days, m.Notes, boolInt(m.Active), m.StartDate, m.EndDate, boolInt(m.Remind), m.Stock, m.Color)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	m.UserID = uid
	return nil
}

// UpdateMedication saves edits.
func (s *Store) UpdateMedication(ctx context.Context, uid int64, m *Medication) error {
	m.Times = NormalizeTimes(m.Times)
	if m.Days == "" {
		m.Days = "1234567"
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE medications SET name = ?, dose = ?, form = ?, times = ?, days = ?, notes = ?, active = ?, start_date = ?, end_date = ?, remind = ?, stock = ?, color = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		m.Name, m.Dose, m.Form, m.Times, m.Days, m.Notes, boolInt(m.Active), m.StartDate, m.EndDate, boolInt(m.Remind), m.Stock, m.Color, Now(), m.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMedication removes one with its log.
func (s *Store) DeleteMedication(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM medications WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DueOn reports whether a medication is scheduled on a date.
func (m *Medication) DueOn(date string) bool {
	if !m.Active {
		return false
	}
	if m.StartDate != "" && date < m.StartDate {
		return false
	}
	if m.EndDate != "" && date > m.EndDate {
		return false
	}
	t, err := ParseDate(date)
	if err != nil {
		return false
	}
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return strings.Contains(m.Days, fmt.Sprint(wd))
}

// MedsForDay returns the medications due on a date with their slots filled
// from the log (plus any extra doses logged that day).
func (s *Store) MedsForDay(ctx context.Context, uid int64, date string) ([]Medication, error) {
	meds, err := s.ListMedications(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	logs, err := s.MedLogs(ctx, uid, date, date)
	if err != nil {
		return nil, err
	}
	byMed := map[int64][]MedLog{}
	for _, l := range logs {
		byMed[l.MedicationID] = append(byMed[l.MedicationID], l)
	}
	out := []Medication{}
	for _, m := range meds {
		due := m.DueOn(date)
		ls := byMed[m.ID]
		if !due && len(ls) == 0 {
			continue
		}
		m.Slots = []MedSlot{}
		if due {
			for _, t := range strings.Split(m.Times, ",") {
				if t == "" {
					continue
				}
				slot := MedSlot{Time: t}
				for _, l := range ls {
					if l.Slot == t {
						slot.Status, slot.TakenAt, slot.LogID, slot.Note = l.Status, l.TakenAt, l.ID, l.Note
					}
				}
				m.Slots = append(m.Slots, slot)
			}
		}
		for _, l := range ls {
			if l.Slot == "" {
				m.Slots = append(m.Slots, MedSlot{Time: "", Status: l.Status, TakenAt: l.TakenAt, LogID: l.ID, Note: l.Note})
			}
		}
		m.Adherence = s.adherence(ctx, uid, m, date)
		out = append(out, m)
	}
	return out, nil
}

// adherence is the share of scheduled doses taken in the last 30 days.
func (s *Store) adherence(ctx context.Context, uid int64, m Medication, until string) int {
	from := AddDays(until, -29)
	logs, err := s.MedLogs(ctx, uid, from, until)
	if err != nil {
		return -1
	}
	taken := map[string]bool{}
	for _, l := range logs {
		if l.MedicationID == m.ID && l.Status == "taken" && l.Slot != "" {
			taken[l.Date+" "+l.Slot] = true
		}
	}
	due, got := 0, 0
	today := Today()
	nowClock := time.Now().Format("15:04")
	for d := from; d <= until; d = AddDays(d, 1) {
		if !m.DueOn(d) {
			continue
		}
		for _, t := range strings.Split(m.Times, ",") {
			if t == "" || (d == today && t > nowClock) {
				continue
			}
			due++
			if taken[d+" "+t] {
				got++
			}
		}
	}
	if due == 0 {
		return -1
	}
	return got * 100 / due
}

// LogDose records a dose taken or skipped (replacing the slot's earlier entry).
func (s *Store) LogDose(ctx context.Context, uid int64, l *MedLog) error {
	if _, err := s.GetMedication(ctx, uid, l.MedicationID); err != nil {
		return err
	}
	if l.Status != "skipped" {
		l.Status = "taken"
	}
	if l.TakenAt == "" {
		l.TakenAt = time.Now().Format(time.RFC3339)
	}
	if l.Date == "" {
		l.Date = DateOf(l.TakenAt)
	}
	if l.Slot != "" {
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM med_log WHERE user_id = ? AND medication_id = ? AND date = ? AND slot = ?`, uid, l.MedicationID, l.Date, l.Slot); err != nil {
			return err
		}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO med_log (user_id, medication_id, date, slot, taken_at, status, note) VALUES (?, ?, ?, ?, ?, ?, ?)`, uid, l.MedicationID, l.Date, l.Slot, l.TakenAt, l.Status, l.Note)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	if l.Status == "taken" {
		_, _ = s.DB.ExecContext(ctx, `UPDATE medications SET stock = stock - 1 WHERE id = ? AND user_id = ? AND stock > 0`, l.MedicationID, uid)
	}
	return nil
}

// DeleteDose removes a log entry (untake).
func (s *Store) DeleteDose(ctx context.Context, uid, id int64) error {
	var medID int64
	var status string
	if err := s.DB.QueryRowContext(ctx, `SELECT medication_id, status FROM med_log WHERE id = ? AND user_id = ?`, id, uid).Scan(&medID, &status); err != nil {
		return notFound(err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM med_log WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	if status == "taken" {
		_, _ = s.DB.ExecContext(ctx, `UPDATE medications SET stock = stock + 1 WHERE id = ? AND user_id = ? AND stock >= 0`, medID, uid)
	}
	return nil
}

// MedLogs lists doses in a range.
func (s *Store) MedLogs(ctx context.Context, uid int64, from, to string) ([]MedLog, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.medication_id, m.name, m.dose, l.date, l.slot, l.taken_at, l.status, l.note FROM med_log l JOIN medications m ON m.id = l.medication_id WHERE l.user_id = ? AND l.date >= ? AND l.date <= ? ORDER BY l.date, l.slot, l.taken_at`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MedLog{}
	for rows.Next() {
		var l MedLog
		if err := rows.Scan(&l.ID, &l.MedicationID, &l.Name, &l.Dose, &l.Date, &l.Slot, &l.TakenAt, &l.Status, &l.Note); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ReminderSent reports / records that a dose was nagged.
func (s *Store) ReminderSent(ctx context.Context, uid, medID int64, date, slot string) bool {
	var at string
	return s.DB.QueryRowContext(ctx, `SELECT sent_at FROM med_reminders WHERE user_id = ? AND medication_id = ? AND date = ? AND slot = ?`, uid, medID, date, slot).Scan(&at) == nil
}

// MarkReminderSent records a reminder.
func (s *Store) MarkReminderSent(ctx context.Context, uid, medID int64, date, slot string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO med_reminders (user_id, medication_id, date, slot, sent_at) VALUES (?, ?, ?, ?, ?)`, uid, medID, date, slot, Now())
	return err
}

// ---- symptoms ----

const symptomCols = `id, user_id, name, severity, at, date, duration_min, note, tags, created_at`

func scanSymptom(row interface{ Scan(...any) error }) (Symptom, error) {
	var x Symptom
	if err := row.Scan(&x.ID, &x.UserID, &x.Name, &x.Severity, &x.At, &x.Date, &x.DurationMin, &x.Note, &x.Tags, &x.CreatedAt); err != nil {
		return x, notFound(err)
	}
	return x, nil
}

// ListSymptoms lists symptoms in a range, newest first.
func (s *Store) ListSymptoms(ctx context.Context, uid int64, from, to string, limit int) ([]Symptom, error) {
	q := `SELECT ` + symptomCols + ` FROM symptoms WHERE user_id = ?`
	args := []any{uid}
	if from != "" {
		q += ` AND date >= ?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND date <= ?`
		args = append(args, to)
	}
	q += ` ORDER BY at DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Symptom{}
	for rows.Next() {
		x, err := scanSymptom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CreateSymptom logs one.
func (s *Store) CreateSymptom(ctx context.Context, uid int64, x *Symptom) error {
	if x.At == "" {
		x.At = time.Now().Format(time.RFC3339)
	}
	x.Date = DateOf(x.At)
	if x.Severity < 1 {
		x.Severity = 1
	}
	if x.Severity > 5 {
		x.Severity = 5
	}
	x.Tags = JoinTags(x.Tags)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO symptoms (user_id, name, severity, at, date, duration_min, note, tags) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uid, x.Name, x.Severity, x.At, x.Date, x.DurationMin, x.Note, x.Tags)
	if err != nil {
		return err
	}
	x.ID, _ = res.LastInsertId()
	x.UserID = uid
	return nil
}

// UpdateSymptom saves edits.
func (s *Store) UpdateSymptom(ctx context.Context, uid int64, x *Symptom) error {
	x.Date = DateOf(x.At)
	x.Tags = JoinTags(x.Tags)
	res, err := s.DB.ExecContext(ctx, `UPDATE symptoms SET name = ?, severity = ?, at = ?, date = ?, duration_min = ?, note = ?, tags = ? WHERE id = ? AND user_id = ?`, x.Name, x.Severity, x.At, x.Date, x.DurationMin, x.Note, x.Tags, x.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSymptom removes one.
func (s *Store) DeleteSymptom(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM symptoms WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SymptomNames lists the names used before (for the picker).
func (s *Store) SymptomNames(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name, COUNT(1) c FROM symptoms WHERE user_id = ? GROUP BY name ORDER BY c DESC, name LIMIT 30`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		var c int
		if err := rows.Scan(&n, &c); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
