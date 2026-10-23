package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-health/internal/ingest"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// applyResult is what a push changed.
type applyResult struct {
	Samples  int      `json:"samples"`
	Workouts int      `json:"workouts"`
	Sleep    int      `json:"sleep"`
	Skipped  int      `json:"skipped"`
	Unknown  []string `json:"unknown,omitempty"`
	Days     int      `json:"days"`
}

// applyBatch writes a batch for a person, rolls the days up, checks goals,
// ticks habits and publishes events.
func (s *Server) applyBatch(ctx context.Context, uid int64, b *ingest.Batch, sourceLabel string) (*applyResult, error) {
	res := &applyResult{Skipped: b.Skipped, Unknown: uniq(b.Unknown)}
	st, _ := s.store.GetSettings(ctx, uid)
	n, touched, err := s.store.UpsertSamples(ctx, uid, b.Samples)
	if err != nil {
		return nil, fmt.Errorf("samples: %w", err)
	}
	res.Samples = n
	s.ingested.Add(int64(n))
	if err := s.store.Rollup(ctx, uid, touched, st.PreferDevice); err != nil {
		return nil, fmt.Errorf("rollup: %w", err)
	}
	wn, fresh, err := s.store.UpsertWorkouts(ctx, uid, b.Workouts)
	if err != nil {
		return nil, fmt.Errorf("workouts: %w", err)
	}
	res.Workouts = wn
	sn, freshSleep, err := s.store.UpsertSleep(ctx, uid, b.Sleep)
	if err != nil {
		return nil, fmt.Errorf("sleep: %w", err)
	}
	res.Sleep = sn
	for _, ss := range b.Sleep {
		touched.Merge(store.Touched{"sleep_asleep": {ss.Date: true}})
	}
	res.Days = touched.Count()
	// events for the new things
	for _, id := range fresh {
		if w, err := s.store.GetWorkout(ctx, uid, id); err == nil {
			s.publish(ctx, uid, "health.workout_recorded", fmt.Sprintf("workout:%d", w.ID), map[string]any{
				"workout_id": w.ID, "kind": w.Kind, "name": w.Name, "date": w.Date, "start_at": w.StartAt, "duration_s": w.DurationS,
				"distance_m": w.DistanceM, "energy_kcal": w.EnergyKcal, "avg_hr": w.AvgHR, "source": w.Source})
		}
	}
	for _, id := range freshSleep {
		if x, err := s.store.GetSleep(ctx, uid, id); err == nil {
			s.publish(ctx, uid, "health.sleep_recorded", fmt.Sprintf("sleep:%d", x.ID), map[string]any{
				"sleep_id": x.ID, "date": x.Date, "start_at": x.StartAt, "end_at": x.EndAt, "asleep_min": x.AsleepS / 60, "in_bed_min": x.InBedS / 60, "deep_min": x.DeepS / 60, "rem_min": x.RemS / 60})
		}
	}
	if dates, ok := touched["weight"]; ok {
		for d := range dates {
			if p, err := s.store.LatestDaily(ctx, uid, "weight"); err == nil && p.Date == d {
				s.publish(ctx, uid, "health.weight_recorded", "weight:"+d, map[string]any{"date": d, "kg": p.Value})
			}
		}
	}
	s.checkGoals(ctx, uid, touched)
	if len(fresh) > 0 {
		s.markPendingWorkouts(ctx, uid)
	}
	if sourceLabel != "" && (n > 0 || wn > 0 || sn > 0) {
		s.log.Info("ingested", "user", uid, "source", sourceLabel, "samples", n, "workouts", wn, "sleep", sn, "skipped", b.Skipped)
	}
	return res, nil
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// handleIngestHAE takes a Health Auto Export "REST API" automation body.
func (s *Server) handleIngestHAE(w http.ResponseWriter, r *http.Request) {
	raw, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	b, err := ingest.HAE(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.applyBatch(r.Context(), actorID(r.Context()), b, "health-auto-export")
	if err != nil {
		s.fail(w, err, "ingest")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleIngest takes Pocket Health's own body (apps, Shortcuts, scripts, links).
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	raw, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	// a Health Auto Export body sent here by mistake still works
	if strings.Contains(string(raw[:min(len(raw), 200)]), `"metrics"`) && strings.Contains(string(raw[:min(len(raw), 60)]), `"data"`) {
		s.handleIngestHAEBytes(w, r, raw)
		return
	}
	source := "api"
	if app := linkApp(r.Context()); app != "" {
		source = app
	} else if src := r.URL.Query().Get("source"); src != "" {
		source = sanitizeSource(src)
	}
	b, err := ingest.Pocket(raw, source)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.applyBatch(r.Context(), actorID(r.Context()), b, source)
	if err != nil {
		s.fail(w, err, "ingest")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleIngestHAEBytes(w http.ResponseWriter, r *http.Request, raw []byte) {
	b, err := ingest.HAE(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.applyBatch(r.Context(), actorID(r.Context()), b, "health-auto-export")
	if err != nil {
		s.fail(w, err, "ingest")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func sanitizeSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" || out == "manual" || out == "apple" || out == "hae" {
		return "api"
	}
	return out
}

// handleImportApple takes export.zip / export.xml (multipart "file") and
// parses it in the background; the job id is returned at once.
func (s *Server) handleImportApple(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send the export as multipart/form-data with a \"file\" field")
		return
	}
	dir := filepath.Join(s.cfg.FilesDir, "imports")
	_ = os.MkdirAll(dir, 0o755)
	var path, name, since string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "upload failed: "+err.Error())
			return
		}
		if part.FormName() == "since" {
			b, _ := io.ReadAll(io.LimitReader(part, 32))
			since = strings.TrimSpace(string(b))
			continue
		}
		if part.FormName() != "file" {
			continue
		}
		name = part.FileName()
		ext := ".xml"
		if strings.HasSuffix(strings.ToLower(name), ".zip") {
			ext = ".zip"
		}
		path = filepath.Join(dir, fmt.Sprintf("apple-%d-%s%s", uid, time.Now().Format("20060102-150405"), ext))
		f, err := os.Create(path)
		if err != nil {
			s.fail(w, err, "upload")
			return
		}
		if _, err := io.Copy(f, part); err != nil {
			f.Close()
			os.Remove(path)
			writeErr(w, http.StatusBadRequest, "upload failed: "+err.Error())
			return
		}
		f.Close()
	}
	if path == "" {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	if !validDate(since) {
		since = ""
	}
	job, err := s.store.CreateImport(ctx, uid, "apple", name)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	go s.runAppleImport(uid, job.ID, path, since)
	writeJSON(w, http.StatusAccepted, job)
}

type importSink struct {
	s   *Server
	uid int64
	job *store.Import
	ctx context.Context
}

func (k *importSink) Samples(ctx context.Context, in []store.Sample) error {
	n, _, err := k.s.store.UpsertSamples(ctx, k.uid, in)
	k.job.Samples += n
	return err
}
func (k *importSink) Workouts(ctx context.Context, in []store.Workout) error {
	for i := range in {
		in[i].HabitMarkedAt = store.Now() // history never ticks habits
	}
	n, _, err := k.s.store.UpsertWorkouts(ctx, k.uid, in)
	k.job.Workouts += n
	return err
}
func (k *importSink) Sleep(ctx context.Context, in []store.SleepSession) error {
	n, _, err := k.s.store.UpsertSleep(ctx, k.uid, in)
	k.job.Sleep += n
	return err
}
func (k *importSink) Progress(ctx context.Context, text string) {
	k.job.Progress = text
	_ = k.s.store.UpdateImport(ctx, k.job)
}

func (s *Server) runAppleImport(uid, jobID int64, path, since string) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	job, err := s.store.GetImport(ctx, uid, jobID)
	if err != nil {
		return
	}
	defer os.Remove(path)
	finish := func(err error) {
		job.FinishedAt = store.Now()
		if err != nil {
			job.Status, job.Error = "error", err.Error()
			s.log.Warn("apple import failed", "user", uid, "err", err)
		} else {
			job.Status = "done"
		}
		_ = s.store.UpdateImport(ctx, job)
	}
	rc, err := ingest.AppleFile(path)
	if err != nil {
		finish(err)
		return
	}
	defer rc.Close()
	sink := &importSink{s: s, uid: uid, job: job, ctx: ctx}
	stats, err := ingest.Apple(ctx, rc, sink, since)
	if stats != nil {
		job.Skipped = stats.Skipped
		var unk []string
		for k := range stats.Unknown {
			unk = append(unk, k)
		}
		if len(unk) > 0 {
			job.Progress = fmt.Sprintf("%d records; kept as-is: %s", stats.Records, strings.Join(unk, ", "))
		} else {
			job.Progress = fmt.Sprintf("%d records", stats.Records)
		}
	}
	if err != nil {
		finish(err)
		return
	}
	job.Progress += " · rolling days up"
	_ = s.store.UpdateImport(ctx, job)
	st, _ := s.store.GetSettings(ctx, uid)
	if err := s.store.RollupAll(ctx, uid, st.PreferDevice); err != nil {
		finish(err)
		return
	}
	s.ingested.Add(int64(job.Samples))
	finish(nil)
	s.publish(ctx, uid, "health.imported", fmt.Sprintf("import:%d", job.ID), map[string]any{"kind": "apple", "samples": job.Samples, "workouts": job.Workouts, "sleep": job.Sleep})
	s.log.Info("apple health import done", "user", uid, "samples", job.Samples, "workouts", job.Workouts, "sleep", job.Sleep, "skipped", job.Skipped)
}

// handleImportCSV reads a CSV (multipart "file" or the raw body) for one
// metric (?metric=weight) or with a metric column.
func (s *Server) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	metric := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("metric")))
	source := sanitizeSource(firstNonEmpty(r.URL.Query().Get("source"), "csv"))
	var rd io.Reader = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(s.cfg.MaxBodyBytes); err != nil {
			writeErr(w, http.StatusBadRequest, "upload failed: "+err.Error())
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "no file")
			return
		}
		defer f.Close()
		rd = f
		if m := r.FormValue("metric"); m != "" {
			metric = strings.ToLower(strings.TrimSpace(m))
		}
	}
	b, err := ingest.CSV(rd, metric, source)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.applyBatch(ctx, uid, b, "csv")
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListImports(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListImports(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "imports")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetImport(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetImport(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleSources lists where the data came from and when each last wrote.
func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	last, err := s.store.LastSync(ctx, uid)
	if err != nil {
		s.fail(w, err, "sources")
		return
	}
	devices, _ := s.store.Devices(ctx, uid)
	imports, _ := s.store.ListImports(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]any{"last_sync": last, "devices": devices, "imports": imports, "base_url": s.cfg.BaseURL})
}

// handleDeleteSource removes everything one source wrote (undo an import).
func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	src := chi.URLParam(r, "source")
	if src == "" {
		writeErr(w, http.StatusBadRequest, "which source?")
		return
	}
	n, err := s.store.DeleteSource(ctx, uid, src)
	if err != nil {
		s.fail(w, err, "delete source")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if err := s.store.RollupAll(ctx, uid, st.PreferDevice); err != nil {
		s.fail(w, err, "rollup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// handleRollup recomputes every day (after changing the preferred device, or just in case).
func (s *Server) handleRollup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	if err := s.store.RollupAll(ctx, uid, st.PreferDevice); err != nil {
		s.fail(w, err, "rollup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
