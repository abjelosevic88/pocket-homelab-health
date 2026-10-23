package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-health/internal/ingest"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

func (s *Server) handleListMedications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListMedications(ctx, actorID(ctx), queryBool(r, "all"))
	if err != nil {
		s.fail(w, err, "medications")
		return
	}
	for i := range list {
		list[i].Adherence = -1
	}
	writeJSON(w, http.StatusOK, list)
}

type medInput struct {
	Name      string `json:"name"`
	Dose      string `json:"dose"`
	Form      string `json:"form"`
	Times     string `json:"times"`
	Days      string `json:"days"`
	Notes     string `json:"notes"`
	Active    *bool  `json:"active"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Remind    *bool  `json:"remind"`
	Stock     *int   `json:"stock"`
	Color     string `json:"color"`
}

func (in *medInput) apply(m *store.Medication, keys map[string]bool) error {
	if keys["name"] {
		m.Name = strings.TrimSpace(in.Name)
	}
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if keys["dose"] {
		m.Dose = strings.TrimSpace(in.Dose)
	}
	if keys["form"] {
		m.Form = strings.TrimSpace(in.Form)
	}
	if keys["times"] {
		m.Times = store.NormalizeTimes(in.Times)
	}
	if keys["days"] {
		var b strings.Builder
		for _, c := range in.Days {
			if c >= '1' && c <= '7' && !strings.ContainsRune(b.String(), c) {
				b.WriteRune(c)
			}
		}
		m.Days = b.String()
		if m.Days == "" {
			m.Days = "1234567"
		}
	}
	if keys["notes"] {
		m.Notes = strings.TrimSpace(in.Notes)
	}
	if in.Active != nil {
		m.Active = *in.Active
	}
	if keys["start_date"] && validDate(in.StartDate) {
		m.StartDate = in.StartDate
	}
	if keys["end_date"] && validDate(in.EndDate) {
		m.EndDate = in.EndDate
	}
	if in.Remind != nil {
		m.Remind = *in.Remind
	}
	if in.Stock != nil {
		m.Stock = *in.Stock
	}
	if keys["color"] {
		m.Color = strings.TrimSpace(in.Color)
	}
	return nil
}

func (s *Server) handleCreateMedication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in medInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m := store.Medication{Active: true, Remind: true, Stock: -1, Times: "08:00", Days: "1234567"}
	if err := in.apply(&m, keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateMedication(ctx, actorID(ctx), &m); err != nil {
		s.fail(w, err, "create medication")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleUpdateMedication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMedication(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "medication")
		return
	}
	var in medInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := in.apply(m, keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateMedication(ctx, uid, m); err != nil {
		s.fail(w, err, "update medication")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMedication(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMedication(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete medication")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMedsDay lists the doses of a day with their status.
func (s *Server) handleMedsDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := chi.URLParam(r, "date")
	if date == "today" || date == "" {
		date = store.Today()
	}
	if !validDate(date) {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	list, err := s.store.MedsForDay(ctx, actorID(ctx), date)
	if err != nil {
		s.fail(w, err, "medications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "medications": list})
}

func (s *Server) handleMedLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := dateRange(r, 30)
	list, err := s.store.MedLogs(ctx, actorID(ctx), from, to)
	if err != nil {
		s.fail(w, err, "medication log")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleLogDose records a dose: {"date":"…","slot":"08:00","status":"taken"|"skipped","taken_at":"…","note":""}.
func (s *Server) handleLogDose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Date    string `json:"date"`
		Slot    string `json:"slot"`
		Status  string `json:"status"`
		TakenAt string `json:"taken_at"`
		Note    string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !validDate(in.Date) {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	l := store.MedLog{MedicationID: idParam(r, "id"), Date: in.Date, Slot: store.NormalizeTimes(in.Slot), Status: in.Status, Note: strings.TrimSpace(in.Note)}
	if t, ok := ingest.ParseTime(in.TakenAt); ok {
		l.TakenAt = ingest.RFC(t)
	}
	if err := s.store.LogDose(ctx, uid, &l); err != nil {
		s.fail(w, err, "log dose")
		return
	}
	if l.Status == "taken" {
		if m, err := s.store.GetMedication(ctx, uid, l.MedicationID); err == nil {
			s.publish(ctx, uid, "health.medication_taken", fmt.Sprintf("dose:%d", l.ID), map[string]any{"medication": m.Name, "dose": m.Dose, "date": l.Date, "slot": l.Slot})
		}
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteDose(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDose(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete dose")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- symptoms ----

func (s *Server) handleListSymptoms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := "", ""
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" || r.URL.Query().Get("range") != "" {
		from, to = dateRange(r, rangeDays(r.URL.Query().Get("range")))
	}
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	list, err := s.store.ListSymptoms(ctx, actorID(ctx), from, to, limit)
	if err != nil {
		s.fail(w, err, "symptoms")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type symptomInput struct {
	Name        string `json:"name"`
	Severity    int    `json:"severity"`
	At          string `json:"at"`
	DurationMin int    `json:"duration_min"`
	Note        string `json:"note"`
	Tags        string `json:"tags"`
}

func (s *Server) handleCreateSymptom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in symptomInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	x := store.Symptom{Name: in.Name, Severity: in.Severity, DurationMin: in.DurationMin, Note: strings.TrimSpace(in.Note), Tags: in.Tags}
	if t, ok := ingest.ParseTime(in.At); ok {
		x.At = ingest.RFC(t)
	}
	if x.Severity == 0 {
		x.Severity = 2
	}
	if err := s.store.CreateSymptom(ctx, uid, &x); err != nil {
		s.fail(w, err, "symptom")
		return
	}
	s.publish(ctx, uid, "health.symptom_logged", fmt.Sprintf("symptom:%d", x.ID), map[string]any{"name": x.Name, "severity": x.Severity, "date": x.Date})
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) handleUpdateSymptom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	list, err := s.store.ListSymptoms(ctx, uid, "", "", 0)
	if err != nil {
		s.fail(w, err, "symptom")
		return
	}
	var x *store.Symptom
	id := idParam(r, "id")
	for i := range list {
		if list[i].ID == id {
			x = &list[i]
		}
	}
	if x == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in symptomInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["name"] && strings.TrimSpace(in.Name) != "" {
		x.Name = strings.TrimSpace(in.Name)
	}
	if keys["severity"] && in.Severity >= 1 && in.Severity <= 5 {
		x.Severity = in.Severity
	}
	if keys["at"] {
		if t, ok := ingest.ParseTime(in.At); ok {
			x.At = ingest.RFC(t)
		}
	}
	if keys["duration_min"] {
		x.DurationMin = in.DurationMin
	}
	if keys["note"] {
		x.Note = strings.TrimSpace(in.Note)
	}
	if keys["tags"] {
		x.Tags = in.Tags
	}
	if err := s.store.UpdateSymptom(ctx, uid, x); err != nil {
		s.fail(w, err, "update symptom")
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) handleDeleteSymptom(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSymptom(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete symptom")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSymptomNames(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.SymptomNames(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "symptoms")
		return
	}
	common := []string{"Headache", "Fatigue", "Nausea", "Back pain", "Sore throat", "Cough", "Fever", "Dizziness", "Stomach ache", "Allergy", "Insomnia", "Migraine", "Muscle soreness", "Anxiety", "Cold"}
	seen := map[string]bool{}
	out := []string{}
	for _, n := range append(names, common...) {
		if !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
