package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-health/internal/metrics"
	"github.com/abjelosevic88/pocket-homelab-health/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-health/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the tokens
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	if st.HomeMetrics == nil {
		st.HomeMetrics = []string{}
	}
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "units": st.Units, "height_cm": st.HeightCm, "birth_date": st.BirthDate, "sex": st.Sex,
		"date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart, "home_metrics": st.HomeMetrics,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_goals": st.NotifyGoals, "notify_weekly": st.NotifyWeekly,
		"notify_meds": st.NotifyMeds, "notify_inactive": st.NotifyInactive, "notify_hour": st.NotifyHour, "weekly_day": st.WeeklyDay, "notify_configured": st.NotifyURL != "",
		"habits_url": st.HabitsURL, "habits_token": mask(st.HabitsToken), "habits_public_url": st.HabitsPublicURL,
		"movement_url": st.MovementURL, "movement_token": mask(st.MovementToken), "movement_public_url": st.MovementPublicURL,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"weather_enabled": st.WeatherEnabled, "weather_lat": st.WeatherLat, "weather_lon": st.WeatherLon, "weather_place": st.WeatherPlace, "weather_last": st.WeatherLast,
		"prefer_device":    st.PreferDevice,
		"max_upload_bytes": s.cfg.MaxUploadBytes, "setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["units"] {
		if in.Units != "metric" && in.Units != "imperial" {
			writeErr(w, http.StatusBadRequest, "units must be metric or imperial")
			return
		}
		cur.Units = in.Units
	}
	if keys["height_cm"] && in.HeightCm >= 0 && in.HeightCm < 300 {
		cur.HeightCm = in.HeightCm
	}
	if keys["birth_date"] && validDate(in.BirthDate) {
		cur.BirthDate = in.BirthDate
	}
	if keys["sex"] && (in.Sex == "" || in.Sex == "female" || in.Sex == "male" || in.Sex == "other") {
		cur.Sex = in.Sex
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if keys["home_metrics"] {
		var hm []string
		for _, k := range in.HomeMetrics {
			if metrics.Known(k) || k != "" {
				hm = append(hm, k)
			}
		}
		if hm == nil {
			hm = []string{}
		}
		cur.HomeMetrics = hm
	}
	intIn := func(k string, dst *int, v, lo, hi int) {
		if keys[k] && v >= lo && v <= hi {
			*dst = v
		}
	}
	intIn("notify_hour", &cur.NotifyHour, in.NotifyHour, 0, 23)
	intIn("weekly_day", &cur.WeeklyDay, in.WeeklyDay, 1, 7)
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	boolIn("notify_goals", &cur.NotifyGoals, in.NotifyGoals)
	boolIn("notify_weekly", &cur.NotifyWeekly, in.NotifyWeekly)
	boolIn("notify_meds", &cur.NotifyMeds, in.NotifyMeds)
	boolIn("notify_inactive", &cur.NotifyInactive, in.NotifyInactive)
	url("habits_url", &cur.HabitsURL, in.HabitsURL)
	secret("habits_token", &cur.HabitsToken, in.HabitsToken)
	url("habits_public_url", &cur.HabitsPublicURL, in.HabitsPublicURL)
	url("movement_url", &cur.MovementURL, in.MovementURL)
	secret("movement_token", &cur.MovementToken, in.MovementToken)
	url("movement_public_url", &cur.MovementPublicURL, in.MovementPublicURL)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, http.StatusBadRequest, "AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, http.StatusBadRequest, "unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	boolIn("weather_enabled", &cur.WeatherEnabled, in.WeatherEnabled)
	if keys["weather_lat"] && in.WeatherLat >= -90 && in.WeatherLat <= 90 {
		cur.WeatherLat = in.WeatherLat
	}
	if keys["weather_lon"] && in.WeatherLon >= -180 && in.WeatherLon <= 180 {
		cur.WeatherLon = in.WeatherLon
	}
	text("weather_place", &cur.WeatherPlace, in.WeatherPlace)
	if keys["weather_lat"] || keys["weather_lon"] {
		cur.WeatherLast = "" // fetch again soon
	}
	text("prefer_device", &cur.PreferDevice, in.PreferDevice)
	mode := func(k string, dst *string, v string) {
		if keys[k] && (v == modeCore || v == modeDirect || v == modeOff) {
			*dst = v
		}
	}
	mode("habits_mode", &cur.HabitsMode, in.HabitsMode)
	mode("movement_mode", &cur.MovementMode, in.MovementMode)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	if keys["prefer_device"] {
		go func(uid int64, prefer string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := s.store.RollupAll(ctx, uid, prefer); err != nil {
				s.log.Warn("rollup after device change", "err", err)
			}
		}(me.ID, cur.PreferDevice)
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, actorID(ctx)).conn(ctx, a, a.scopes[0])
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				switch a.slug {
				case "habits":
					var hs []pocket.Habit
					hs, err = conn.client.Habits(ctx)
					msg = fmt.Sprintf("Pocket Habits %s, %s, %d habits", v, how, len(hs))
				case "movement":
					var pts []pocket.Point
					pts, err = conn.client.Points(ctx, time.Now().Add(-24*time.Hour), time.Now(), 2000)
					msg = fmt.Sprintf("Pocket Movement %s, %s, %d positions in the last 24 h", v, how, len(pts))
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Health", Body: "Notifications work. Met goals, medication reminders and the weekly report will arrive here.", Priority: "info", Tags: []string{"heart"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Health", Body: "Notifications work.", Priority: "info", Tags: []string{"heart"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size, "files_bytes": files,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "ingested": s.ingested.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-health-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's data.
type exportDoc struct {
	App         string               `json:"app"`
	Version     string               `json:"version"`
	Exported    string               `json:"exported_at"`
	Samples     []store.Sample       `json:"samples"`
	Workouts    []store.Workout      `json:"workouts"`
	Sleep       []store.SleepSession `json:"sleep"`
	Goals       []store.Goal         `json:"goals"`
	Medications []store.Medication   `json:"medications"`
	MedLog      []store.MedLog       `json:"med_log"`
	Symptoms    []store.Symptom      `json:"symptoms"`
	Rules       []store.HabitRule    `json:"habit_rules"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-health", Version: Version, Exported: store.Now()}
	doc.Samples, _ = s.store.Samples(ctx, uid, store.SampleFilter{})
	doc.Workouts, _ = s.store.ListWorkouts(ctx, uid, store.WorkoutFilter{})
	doc.Sleep, _ = s.store.ListSleep(ctx, uid, "", "", 0)
	doc.Goals, _ = s.store.ListGoals(ctx, uid)
	doc.Medications, _ = s.store.ListMedications(ctx, uid, true)
	doc.MedLog, _ = s.store.MedLogs(ctx, uid, "0000-01-01", "9999-12-31")
	doc.Symptoms, _ = s.store.ListSymptoms(ctx, uid, "", "", 0)
	doc.Rules, _ = s.store.ListRules(ctx, uid)
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-health-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes the daily numbers as a wide CSV (one row per day).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	stats, err := s.store.MetricStats(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	from := s.store.FirstDate(ctx, uid)
	if from == "" {
		from = store.Today()
	}
	keys := make([]string, 0, len(stats))
	for _, st := range stats {
		keys = append(keys, st.Metric)
	}
	data, err := s.store.DailyMany(ctx, uid, keys, from, store.Today())
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	byDate := map[string]map[string]store.DailyPoint{}
	for k, pts := range data {
		for _, p := range pts {
			if byDate[p.Date] == nil {
				byDate[p.Date] = map[string]store.DailyPoint{}
			}
			byDate[p.Date][k] = p
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-health-daily.csv"`)
	cw := csv.NewWriter(w)
	head := append([]string{"date"}, keys...)
	_ = cw.Write(head)
	for d := from; d <= store.Today(); d = store.AddDays(d, 1) {
		row := byDate[d]
		if row == nil {
			continue
		}
		rec := []string{d}
		for _, k := range keys {
			if p, ok := row[k]; ok {
				v := strconv.FormatFloat(p.Value, 'f', -1, 64)
				if p.Value2 != nil {
					v += "/" + strconv.FormatFloat(*p.Value2, 'f', -1, 64)
				}
				rec = append(rec, v)
			} else {
				rec = append(rec, "")
			}
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}

// handleImportJSON merges an export into the caller's own data.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{}
	for i := range doc.Samples {
		doc.Samples[i].ID = 0
	}
	n, touched, err := s.store.UpsertSamples(ctx, uid, doc.Samples)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	counts["samples"] = n
	st, _ := s.store.GetSettings(ctx, uid)
	if err := s.store.Rollup(ctx, uid, touched, st.PreferDevice); err != nil {
		s.fail(w, err, "import")
		return
	}
	for i := range doc.Workouts {
		doc.Workouts[i].ID = 0
		doc.Workouts[i].HabitMarkedAt = store.Now() // never re-tick habits for imported rows
	}
	counts["workouts"], _, _ = s.store.UpsertWorkouts(ctx, uid, doc.Workouts)
	for i := range doc.Sleep {
		doc.Sleep[i].ID = 0
	}
	counts["sleep"], _, _ = s.store.UpsertSleep(ctx, uid, doc.Sleep)
	for i := range doc.Goals {
		g := doc.Goals[i]
		if err := s.store.UpsertGoal(ctx, uid, &g); err == nil {
			counts["goals"]++
		}
	}
	medIDs := map[int64]int64{}
	for _, m := range doc.Medications {
		old := m.ID
		m.ID = 0
		if err := s.store.CreateMedication(ctx, uid, &m); err == nil {
			medIDs[old] = m.ID
			counts["medications"]++
		}
	}
	for _, l := range doc.MedLog {
		if nid, ok := medIDs[l.MedicationID]; ok {
			l.MedicationID = nid
			l.ID = 0
			if err := s.store.LogDose(ctx, uid, &l); err == nil {
				counts["doses"]++
			}
		}
	}
	for _, x := range doc.Symptoms {
		x.ID = 0
		if err := s.store.CreateSymptom(ctx, uid, &x); err == nil {
			counts["symptoms"]++
		}
	}
	for _, ru := range doc.Rules {
		ru.ID = 0
		if err := s.store.CreateRule(ctx, uid, &ru); err == nil {
			counts["rules"]++
		}
	}
	s.checkGoals(ctx, uid, touched)
	writeJSON(w, http.StatusOK, counts)
}
