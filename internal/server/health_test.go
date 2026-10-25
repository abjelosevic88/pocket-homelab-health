package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-health/internal/config"
	"github.com/abjelosevic88/pocket-homelab-health/internal/ingest"
	"github.com/abjelosevic88/pocket-homelab-health/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://health.test"
	cfg.MaxBodyBytes = 64 << 20
	cfg.MaxUploadBytes = 64 << 20
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

func today() string { return store.Today() }

const haeBody = `{"data":{"metrics":[
 {"name":"step_count","units":"count","data":[{"date":"%s 00:00:00 +0000","qty":9123,"source":"Apple Watch"},{"date":"%s 00:00:00 +0000","qty":4000,"source":"iPhone"}]},
 {"name":"weight_body_mass","units":"lb","data":[{"date":"%s 07:30:00 +0000","qty":176.37}]},
 {"name":"heart_rate","units":"count/min","data":[{"date":"%s 00:00:00 +0000","Avg":71,"Min":52,"Max":140}]},
 {"name":"blood_pressure","units":"mmHg","data":[{"date":"%s 08:00:00 +0000","systolic":121,"diastolic":79}]},
 {"name":"sleep_analysis","units":"hr","data":[{"date":"%s 00:00:00 +0000","sleepStart":"%s 23:10:00 +0000","sleepEnd":"%s 06:40:00 +0000","asleep":6.8,"inBed":7.5,"deep":1.1,"rem":1.4,"core":4.3,"awake":0.7,"source":"Apple Watch"}]},
 {"name":"some_new_thing","units":"widgets","data":[{"date":"%s 10:00:00 +0000","qty":3}]}
],"workouts":[{"name":"Running","start":"%s 07:00:00 +0000","end":"%s 07:35:00 +0000","activeEnergyBurned":{"qty":310,"units":"kcal"},"distance":{"qty":5.2,"units":"km"},"heartRate":{"avg":151,"max":171}}]}}`

func hae(day string) string {
	y := store.AddDays(day, -1)
	return fmt.Sprintf(haeBody, day, day, day, day, day, day, y, day, day, day, day)
}

func TestHealthAutoExportIngestAndRollup(t *testing.T) {
	a := newApp(t, nil)
	d := today()
	res := a.ok(a.alice, "POST", "/api/v1/ingest/hae", hae(d))
	if num(res, "samples") != 6 || num(res, "workouts") != 1 || num(res, "sleep") != 1 {
		t.Fatalf("counts = %v", res)
	}
	day := a.ok(a.alice, "GET", "/api/v1/day/"+d, nil)
	m := day["metrics"].(map[string]any)
	if v := m["steps"].(map[string]any)["value"].(float64); v != 9123 {
		t.Fatalf("steps = %v (iPhone double-count?)", v)
	}
	if v := m["weight"].(map[string]any)["value"].(float64); v < 79.9 || v > 80.1 {
		t.Fatalf("weight = %v, want ~80 kg from lb", v)
	}
	if v := m["blood_pressure"].(map[string]any); v["value"].(float64) != 121 || v["value2"].(float64) != 79 {
		t.Fatalf("bp = %v", v)
	}
	if v := m["sleep_asleep"].(map[string]any)["value"].(float64); v < 407 || v > 409 {
		t.Fatalf("sleep = %v, want 408 min", v)
	}
	if _, ok := m["some_new_thing"]; !ok {
		t.Fatalf("unknown metric was dropped: %v", m)
	}
	if len(day["workouts"].([]any)) != 1 {
		t.Fatalf("workouts = %v", day["workouts"])
	}
	// steps goal (8000) met → hit recorded
	_, _, goals := a.call(a.alice, "GET", "/api/v1/goals", nil)
	found := false
	for _, g := range goals {
		gm := g.(map[string]any)
		if gm["metric"] == "steps" {
			found = true
			if gm["met"] != true {
				t.Fatalf("steps goal not met: %v", gm)
			}
		}
	}
	if !found {
		t.Fatal("default goals missing")
	}
	// re-sending the same day updates instead of doubling
	a.ok(a.alice, "POST", "/api/v1/ingest/hae", hae(d))
	day = a.ok(a.alice, "GET", "/api/v1/day/"+d, nil)
	if v := day["metrics"].(map[string]any)["steps"].(map[string]any)["value"].(float64); v != 9123 {
		t.Fatalf("steps after resend = %v", v)
	}
	w := a.ok(a.alice, "GET", "/api/v1/workouts?limit=5", nil)
	_ = w
	_, _, ws := a.call(a.alice, "GET", "/api/v1/workouts", nil)
	if len(ws) != 1 {
		t.Fatalf("workouts after resend = %d", len(ws))
	}
	// Bob sees nothing of Alice's
	bday := a.ok(a.bob, "GET", "/api/v1/day/"+d, nil)
	if len(bday["metrics"].(map[string]any)) != 0 {
		t.Fatalf("bob sees alice's metrics: %v", bday)
	}
	wid := int64(ws[0].(map[string]any)["id"].(float64))
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/workouts/%d", wid), nil)
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/workouts/%d", wid), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/workouts/%d", wid), map[string]any{"notes": "x"})
}

func TestManualLogAndMetricPage(t *testing.T) {
	a := newApp(t, nil)
	s := a.expect(201, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "weight", "value": 180, "unit": "lb", "note": "morning"})
	if v := num(s, "value"); v < 81.6 || v > 81.7 {
		t.Fatalf("value = %v", v)
	}
	a.expect(400, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "blood_pressure", "value": 120})
	a.expect(201, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "blood_pressure", "value": 120, "value2": 80})
	page := a.ok(a.alice, "GET", "/api/v1/metrics/weight?range=w", nil)
	if len(page["points"].([]any)) != 1 {
		t.Fatalf("points = %v", page["points"])
	}
	id := int64(num(s, "id"))
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/samples/%d", id), nil)
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/samples/%d", id), nil)
	page = a.ok(a.alice, "GET", "/api/v1/metrics/weight?range=w", nil)
	if len(page["points"].([]any)) != 0 {
		t.Fatalf("points after delete = %v", page["points"])
	}
	_, _, list := a.call(a.alice, "GET", "/api/v1/metrics", nil)
	if len(list) != 1 {
		t.Fatalf("metrics with data = %d, want 1 (blood pressure)", len(list))
	}
}

func TestGenericIngestAndGoals(t *testing.T) {
	a := newApp(t, nil)
	d := today()
	body := map[string]any{
		"samples":  []map[string]any{{"metric": "water", "at": d + "T10:00:00Z", "value": 0.5, "unit": "L"}, {"metric": "water", "at": d + "T14:00:00Z", "value": 1600, "unit": "mL"}},
		"workouts": []map[string]any{{"kind": "cycling", "start": d + "T17:00:00Z", "duration_s": 3600, "distance_m": 25000}},
		"sleep":    []map[string]any{{"start": store.AddDays(d, -1) + "T23:00:00Z", "end": d + "T06:30:00Z", "asleep_min": 420}},
	}
	res := a.ok(a.alice, "POST", "/api/v1/ingest?source=myapp", body)
	if num(res, "samples") != 2 || num(res, "workouts") != 1 || num(res, "sleep") != 1 {
		t.Fatalf("res = %v", res)
	}
	day := a.ok(a.alice, "GET", "/api/v1/day/"+d, nil)
	if v := day["metrics"].(map[string]any)["water"].(map[string]any)["value"].(float64); v != 2100 {
		t.Fatalf("water = %v", v)
	}
	// water goal 2000 met; set a stricter one and it is re-evaluated
	a.ok(a.alice, "PUT", "/api/v1/goals/water", map[string]any{"target": 2500})
	_, _, goals := a.call(a.alice, "GET", "/api/v1/goals", nil)
	for _, g := range goals {
		gm := g.(map[string]any)
		if gm["metric"] == "water" && gm["met"] != false {
			t.Fatalf("water goal should be unmet at 2500: %v", gm)
		}
	}
	a.expect(204, a.alice, "DELETE", "/api/v1/goals/water", nil)
	a.expect(404, a.alice, "DELETE", "/api/v1/goals/water", nil)
	// sources
	src := a.ok(a.alice, "GET", "/api/v1/sources", nil)
	if _, ok := src["last_sync"].(map[string]any)["myapp"]; !ok {
		t.Fatalf("source missing: %v", src)
	}
	a.ok(a.alice, "DELETE", "/api/v1/sources/myapp", nil)
	day = a.ok(a.alice, "GET", "/api/v1/day/"+d, nil)
	if len(day["metrics"].(map[string]any)) != 0 {
		t.Fatalf("source delete left data: %v", day["metrics"])
	}
}

func TestMedicationsAndSymptoms(t *testing.T) {
	a := newApp(t, nil)
	m := a.expect(201, a.alice, "POST", "/api/v1/medications", map[string]any{"name": "Vitamin D", "dose": "2000 IU", "times": "8:00, 20:00", "stock": 10})
	if m["times"] != "08:00,20:00" {
		t.Fatalf("times = %v", m["times"])
	}
	id := int64(num(m, "id"))
	d := today()
	day := a.ok(a.alice, "GET", "/api/v1/medications/day/"+d, nil)
	meds := day["medications"].([]any)
	if len(meds) != 1 || len(meds[0].(map[string]any)["slots"].([]any)) != 2 {
		t.Fatalf("day = %v", day)
	}
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/medications/%d/log", id), map[string]any{"date": d, "slot": "08:00", "status": "taken"})
	day = a.ok(a.alice, "GET", "/api/v1/medications/day/"+d, nil)
	slots := day["medications"].([]any)[0].(map[string]any)["slots"].([]any)
	if slots[0].(map[string]any)["status"] != "taken" || slots[1].(map[string]any)["status"] != "" {
		t.Fatalf("slots = %v", slots)
	}
	got := a.ok(a.alice, "GET", "/api/v1/medications", nil)
	_ = got
	_, _, list := a.call(a.alice, "GET", "/api/v1/medications", nil)
	if int(num(list[0].(map[string]any), "stock")) != 9 {
		t.Fatalf("stock = %v", list[0])
	}
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/medications/%d", id), map[string]any{"name": "x"})
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/medications/%d/log", id), map[string]any{"date": d, "slot": "20:00"})
	sy := a.expect(201, a.alice, "POST", "/api/v1/symptoms", map[string]any{"name": "Headache", "severity": 3})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/symptoms/%d", int64(num(sy, "id"))), nil)
	// medications never leave through the Day endpoint for a linked app: covered by linkApp check; here the owner sees them
	dd := a.ok(a.alice, "GET", "/api/v1/day/"+d, nil)
	if len(dd["symptoms"].([]any)) != 1 {
		t.Fatalf("symptoms in day = %v", dd["symptoms"])
	}
}

func TestLinkScopes(t *testing.T) {
	get := func(p string) *http.Request { r, _ := http.NewRequest("GET", p, nil); return r }
	post := func(p string) *http.Request { r, _ := http.NewRequest("POST", p, nil); return r }
	if !linkAllows([]string{"health:read"}, get("/api/v1/day/2025-10-23")) {
		t.Fatal("read should allow day")
	}
	if linkAllows([]string{"health:read"}, get("/api/v1/medications")) {
		t.Fatal("read must not see medications")
	}
	if linkAllows([]string{"health:read"}, post("/api/v1/ingest")) {
		t.Fatal("read must not write")
	}
	if !linkAllows([]string{"health:write"}, post("/api/v1/ingest")) {
		t.Fatal("write should ingest")
	}
	if linkAllows([]string{"health:write"}, get("/api/v1/metrics")) {
		t.Fatal("write alone must not read")
	}
}

func TestAppleExportParse(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_US">
 <ExportDate value="2025-10-23 10:00:00 +0200"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Aleksandar’s Apple Watch" unit="count" startDate="2025-10-20 08:00:00 +0200" endDate="2025-10-20 08:10:00 +0200" value="300"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Aleksandar’s Apple Watch" unit="count" startDate="2025-10-20 08:20:00 +0200" endDate="2025-10-20 08:30:00 +0200" value="400"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2025-10-20 08:05:00 +0200" endDate="2025-10-20 08:15:00 +0200" value="250"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="lb" startDate="2025-10-20 07:00:00 +0200" endDate="2025-10-20 07:00:00 +0200" value="176.37"/>
 <Record type="HKQuantityTypeIdentifierBloodPressureSystolic" sourceName="Withings" unit="mmHg" startDate="2025-10-20 07:05:00 +0200" endDate="2025-10-20 07:05:00 +0200" value="122"/>
 <Record type="HKQuantityTypeIdentifierBloodPressureDiastolic" sourceName="Withings" unit="mmHg" startDate="2025-10-20 07:05:00 +0200" endDate="2025-10-20 07:05:00 +0200" value="78"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Aleksandar’s Apple Watch" startDate="2025-10-19 23:00:00 +0200" endDate="2025-10-20 01:00:00 +0200" value="HKCategoryValueSleepAnalysisAsleepCore"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Aleksandar’s Apple Watch" startDate="2025-10-20 01:00:00 +0200" endDate="2025-10-20 02:00:00 +0200" value="HKCategoryValueSleepAnalysisAsleepDeep"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Aleksandar’s Apple Watch" startDate="2025-10-20 02:00:00 +0200" endDate="2025-10-20 06:30:00 +0200" value="HKCategoryValueSleepAnalysisAsleepREM"/>
 <Record type="HKQuantityTypeIdentifierUnknownFuture" sourceName="iPhone" unit="widgets" startDate="2025-10-20 09:00:00 +0200" endDate="2025-10-20 09:00:00 +0200" value="7"/>
 <Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="35.5" durationUnit="min" sourceName="Aleksandar’s Apple Watch" startDate="2025-10-20 07:30:00 +0200" endDate="2025-10-20 08:05:30 +0200">
  <WorkoutStatistics type="HKQuantityTypeIdentifierDistanceWalkingRunning" sum="5.2" unit="km"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierActiveEnergyBurned" sum="310" unit="kcal"/>
  <WorkoutStatistics type="HKQuantityTypeIdentifierHeartRate" average="151" maximum="171" unit="count/min"/>
  <MetadataEntry key="HKElevationAscended" value="4200 cm"/>
 </Workout>
</HealthData>`
	a := newApp(t, nil)
	sink := &importSink{s: a.s, uid: a.ids["Alice"], job: &store.Import{}, ctx: context.Background()}
	st, err := ingest.Apple(context.Background(), strings.NewReader(xml), sink, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Workouts != 1 || st.Sleep != 1 {
		t.Fatalf("stats = %+v", st)
	}
	if err := a.st.RollupAll(context.Background(), a.ids["Alice"], ""); err != nil {
		t.Fatal(err)
	}
	day := a.ok(a.alice, "GET", "/api/v1/day/2025-10-20", nil)
	m := day["metrics"].(map[string]any)
	if v := m["steps"].(map[string]any)["value"].(float64); v != 700 {
		t.Fatalf("steps = %v (want the watch's 700, not 950)", v)
	}
	if v := m["weight"].(map[string]any)["value"].(float64); v < 79.9 || v > 80.1 {
		t.Fatalf("weight = %v", v)
	}
	if v := m["blood_pressure"].(map[string]any); v["value"].(float64) != 122 || v["value2"].(float64) != 78 {
		t.Fatalf("bp = %v", v)
	}
	if v := m["sleep_asleep"].(map[string]any)["value"].(float64); v != 450 {
		t.Fatalf("sleep = %v, want 450", v)
	}
	if _, ok := m["unknown_future"]; !ok {
		t.Fatalf("unknown apple type dropped: %v", m)
	}
	ws := day["workouts"].([]any)
	w := ws[0].(map[string]any)
	if num(w, "distance_m") != 5200 || num(w, "avg_hr") != 151 || int(num(w, "elevation_m")) != 42 || num(w, "duration_s") != 2130 {
		t.Fatalf("workout = %v", w)
	}
}

func TestCSVImport(t *testing.T) {
	a := newApp(t, nil)
	csv := "date,weight,unit\n2025-10-01,80.5,kg\n2025-10-02,80.1,kg\nbad,row\n"
	req, _ := http.NewRequest("POST", a.url+"/api/v1/import/csv?metric=weight", strings.NewReader(csv))
	req.Header.Set("Authorization", "Bearer "+a.alice)
	req.Header.Set("Content-Type", "text/csv")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if num(out, "samples") != 2 || num(out, "skipped") != 1 {
		t.Fatalf("csv = %v", out)
	}
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"units": "imperial", "height_cm": 181, "habits_token": "ph_secret", "title": "Family Health"})
	bob := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if bob["units"] != "metric" || bob["habits_token"] != "" || bob["title"] != "Family Health" {
		t.Fatalf("bob's settings leak: %v", bob)
	}
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"title": "Bob's"})
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if al["title"] != "Family Health" || al["habits_token"] == "" || strings.Contains(al["habits_token"].(string), "secret") {
		t.Fatalf("alice settings = %v", al)
	}
	_ = time.Now
}

func TestRouteSimplify(t *testing.T) {
	// Sarajevo: 1 km east is about 0.0124° of longitude
	pts := []pocket.Point{{Lat: 43.85, Lng: 18.38, TS: 0}, {Lat: 43.85, Lng: 18.38001, TS: 1}, {Lat: 43.85, Lng: 18.3924, TS: 300}, {Lat: 43.95, Lng: 18.3924, TS: 301}, {Lat: 43.85, Lng: 18.4048, TS: 600}}
	line, dist := simplify(pts)
	if len(line) != 3 {
		t.Fatalf("line = %v (jitter and the jump should be dropped)", line)
	}
	if dist < 1900 || dist > 2100 {
		t.Fatalf("distance = %v, want ~2000 m", dist)
	}
}

// Mindful minutes from different sources (the watch, Pocket Still) add up;
// Still zeroing a deleted practice takes its minutes out again.
func TestMindfulMinutesSumAcrossSources(t *testing.T) {
	a := newApp(t, nil)
	day := time.Now().Add(-3 * time.Hour)
	if day.Format("2006-01-02") != today() {
		day = time.Now().Add(-time.Minute)
	}
	at := day.Format(time.RFC3339)
	a.expect(201, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "mindful_min", "value": 10, "at": at, "source": "still"})
	a.expect(201, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "mindful_min", "value": 5, "at": day.Add(-time.Minute).Format(time.RFC3339), "source": "hae"})
	page := a.ok(a.alice, "GET", "/api/v1/metrics/mindful_min?range=w", nil)
	pts := page["points"].([]any)
	if len(pts) != 1 || num(pts[0].(map[string]any), "value") != 15 {
		t.Fatalf("points = %v", pts)
	}
	a.expect(201, a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "mindful_min", "value": 0, "at": at, "source": "still"})
	page = a.ok(a.alice, "GET", "/api/v1/metrics/mindful_min?range=w", nil)
	if v := num(page["points"].([]any)[0].(map[string]any), "value"); v != 5 {
		t.Fatalf("after zeroing = %v", v)
	}
}
