package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-health/internal/config"
	"github.com/abjelosevic88/pocket-homelab-health/internal/store"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Health registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "health:read", "write": "health:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "health", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" }), fc
}

// capItems are the rows of a capability answer.
func capItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// capItem finds the row with that title.
func capItem(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range capItems(t, m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

// aliceDay gives Alice a day of health data today (and a planned workout
// yesterday), plus the things that must never leave the app: a medication,
// a symptom and a cycle-tracking reading. Bob gets a workout of his own.
func aliceDay(a *app) {
	a.t.Helper()
	d := today()
	y := store.AddDays(d, -1)
	a.ok(a.alice, "PUT", "/api/v1/goals/steps", map[string]any{"target": 8000})
	a.ok(a.alice, "PUT", "/api/v1/goals/resting_hr", map[string]any{"target": 70, "direction": "at_most"})
	a.ok(a.alice, "POST", "/api/v1/ingest?source=phone", map[string]any{
		"samples": []map[string]any{
			{"metric": "steps", "at": d + "T10:00:00Z", "value": 6240},
			{"metric": "resting_hr", "at": d + "T10:00:00Z", "value": 52},
			{"metric": "active_energy", "at": d + "T10:00:00Z", "value": 410},
			{"metric": "weight", "at": d + "T07:00:00Z", "value": 82.4, "unit": "kg"},
			{"metric": "cervical_mucus", "at": d + "T07:00:00Z", "value": 3},
		},
		"workouts": []map[string]any{
			{"kind": "running", "name": "Morning run", "start": d + "T07:00:00Z", "end": d + "T07:35:00Z", "duration_s": 2100, "distance_m": 5200, "energy_kcal": 310, "notes": "Felt strong along the river"},
			{"kind": "running", "name": "5 x 400 m", "plan_id": "plan-1", "plan_name": "5 x 400 m", "start": y + "T17:00:00Z", "end": y + "T17:40:00Z", "duration_s": 2400, "distance_m": 6100},
		},
		"sleep": []map[string]any{{"start": y + "T23:10:00Z", "end": d + "T06:40:00Z", "asleep_min": 440}},
	})
	a.expect(201, a.alice, "POST", "/api/v1/medications", map[string]any{"name": "Vitamin D", "dose": "2000 IU", "times": "8:00"})
	a.expect(201, a.alice, "POST", "/api/v1/symptoms", map[string]any{"name": "Headache", "severity": 3})
	a.ok(a.bob, "POST", "/api/v1/ingest?source=phone", map[string]any{
		"workouts": []map[string]any{{"kind": "cycling", "name": "Bob's ride", "start": d + "T09:00:00Z", "duration_s": 3600, "distance_m": 25000}},
	})
}

func TestPocketSearch(t *testing.T) {
	a, _ := newCapApp(t)
	aliceDay(a)
	// a workout by kind, newest first; the planned one is a plan too
	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=run", nil)
	run := capItem(t, res, "Morning run")
	if run["kind"] != "workout" || run["date"] != today() || !strings.HasPrefix(run["url"].(string), "/workouts/") ||
		!strings.HasPrefix(run["snippet"].(string), "today, ") || !strings.Contains(run["snippet"].(string), "35 min · 5.20 km · 310 kcal") {
		t.Fatalf("workout: %v", run)
	}
	// by the plan's name: the plan opens all its sessions
	res = a.ok(a.alice, "GET", "/api/v1/pocket/search?q=400", nil)
	got := capItems(t, res)
	if len(got) != 2 || got[0]["kind"] != "plan" || got[0]["url"] != "/workouts?plan=plan-1" || got[0]["snippet"] != "Planned workout · 1 session · last yesterday" || got[1]["kind"] != "workout" {
		t.Fatalf("plan: %v", got)
	}
	// by words in the notes, which the line then shows
	got = capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=river", nil))
	if len(got) != 1 || !strings.HasSuffix(got[0]["snippet"].(string), "Notes: Felt strong along the river") {
		t.Fatalf("notes: %v", got)
	}
	// a measurement's name: one item with its latest value, opening its page
	w := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=weight", nil), "Weight")
	if w["kind"] != "measurement" || w["url"] != "/metrics/weight" || w["snippet"] != "Latest 82.4 kg · today" {
		t.Fatalf("weight: %v", w)
	}
	// in pounds for someone who reads pounds
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"units": "imperial"})
	if w := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=weight", nil), "Weight"); w["snippet"] != "Latest 181.7 lb · today" {
		t.Fatalf("weight in pounds: %v", w)
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"units": "metric"})
	// goals by their measurement, or all of them
	steps := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=steps", nil), "Steps goal")
	if steps["kind"] != "goal" || steps["url"] != "/goals" || steps["snippet"] != "At least 8,000 steps a day · 6,240 today" {
		t.Fatalf("goal: %v", steps)
	}
	goals := 0
	for _, it := range capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=goals", nil)) {
		if it["kind"] == "goal" {
			goals++
		}
	}
	if goals < 2 {
		t.Fatalf("goals: %d", goals)
	}
	// never medications, symptoms or cycle tracking; nothing for no word
	for _, q := range []string{"vitamin", "headache", "cervical", "mucus", "??", "*"} {
		if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Errorf("%q found %v", q, got)
		}
	}
	// Bob finds his own and none of Alice's
	if got := capItems(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=run", nil)); len(got) != 0 {
		t.Fatalf("bob finds alice's runs: %v", got)
	}
	if got := capItems(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=ride", nil)); len(got) != 1 || got[0]["title"] != "Bob's ride" {
		t.Fatalf("bob's ride: %v", got)
	}
	if got := capItems(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=weight", nil)); len(got) != 0 {
		t.Fatalf("bob finds alice's weight: %v", got)
	}
	// the limit holds
	if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=r&limit=2", nil)); len(got) != 2 {
		t.Fatalf("limit: %v", got)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=run", nil)
}

func TestPocketDay(t *testing.T) {
	a, _ := newCapApp(t)
	aliceDay(a)
	d := today()
	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+d+"?tz=UTC", nil)
	if day["date"] != d || day["summary"] != "6240 steps · slept 7h 20m · running 35 min · resting HR 52 · 82.4 kg" {
		t.Fatalf("day: %v", day)
	}
	run := capItem(t, day, "Morning run")
	if run["kind"] != "workout" || run["time"] != "07:00" || run["end"] != "07:35" || run["minutes"] != 35.0 || run["qty"] != 5.2 ||
		run["sub"] != "Running · 35 min · 5.20 km · 310 kcal" || !strings.HasPrefix(run["url"].(string), "/workouts/") || run["note"] != nil {
		t.Fatalf("workout: %v", run)
	}
	sleep := capItem(t, day, "Slept 7h 20m")
	if sleep["kind"] != "sleep" || sleep["sub"] != "23:10 to 06:40" || sleep["url"] != "/sleep" || sleep["time"] != nil || sleep["minutes"] != nil {
		t.Fatalf("sleep: %v", sleep)
	}
	goal := capItem(t, day, "Resting heart rate goal met")
	if goal["done"] != true || goal["kind"] != "goal" || goal["sub"] != "At most 70 bpm a day" {
		t.Fatalf("goal: %v", goal)
	}
	// Alice's own sleep goal (one of the goals she starts with) was met too; the steps goal was not
	if g := capItem(t, day, "Time asleep goal met"); g["sub"] != "At least 7h 00m a day" {
		t.Fatalf("sleep goal: %v", g)
	}
	if len(capItems(t, day)) != 4 {
		t.Fatalf("items: %v", day["items"])
	}
	// the Today page's first tiles, sleep left out (it is an item)
	ms, _ := day["metrics"].([]any)
	want := []string{"Steps=6,240", "Active energy=410 kcal", "Resting heart rate=52 bpm", "Weight=82.4 kg"}
	if len(ms) != len(want) {
		t.Fatalf("metrics: %v", ms)
	}
	for i, m := range ms {
		mm := m.(map[string]any)
		if got := mm["label"].(string) + "=" + mm["value"].(string); got != want[i] {
			t.Errorf("metric %d = %s, want %s", i, got, want[i])
		}
	}
	// nothing private, even for the person
	raw, _ := json.Marshal(day)
	for _, s := range []string{"Vitamin", "Headache", "Cervical", "mucus"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("day carries %s: %s", s, raw)
		}
	}
	// a day with nothing, and Bob's day has only his ride
	if empty := a.ok(a.alice, "GET", "/api/v1/pocket/day/2020-01-01", nil); empty["summary"] != nil || len(capItems(t, empty)) != 0 {
		t.Fatalf("empty day: %v", empty)
	}
	bob := a.ok(a.bob, "GET", "/api/v1/pocket/day/"+d+"?tz=UTC", nil)
	if got := capItems(t, bob); len(got) != 1 || got[0]["title"] != "Bob's ride" || got[0]["qty"] != 25.0 || bob["summary"] != "cycling 60 min" {
		t.Fatalf("bob's day: %v", bob)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/2026-13-40", nil)
	a.expect(401, "", "GET", "/api/v1/pocket/day/"+d, nil)
}

func TestPocketWidget(t *testing.T) {
	a, _ := newCapApp(t)
	// nothing yet today: nothing to show
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || len(capItems(t, w)) != 0 || w["metrics"] != nil {
		t.Fatalf("empty widget: %v", w)
	}
	aliceDay(a)
	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	if w["line"] != "6,240 of 8,000 steps" || w["url"] != "/" {
		t.Fatalf("widget: %v", w)
	}
	ms, _ := w["metrics"].([]any)
	want := []string{"Sleep=7h 20m", "Active energy=410 kcal", "Resting heart rate=52 bpm", "Weight=82.4 kg"}
	if len(ms) != len(want) {
		t.Fatalf("metrics: %v", ms)
	}
	for i, m := range ms {
		mm := m.(map[string]any)
		if got := mm["label"].(string) + "=" + mm["value"].(string); got != want[i] {
			t.Errorf("metric %d = %s, want %s", i, got, want[i])
		}
	}
	run := capItem(t, w, "Morning run")
	if !strings.HasSuffix(run["detail"].(string), "35 min · 5.20 km · 310 kcal") || run["done"] != true {
		t.Fatalf("workout row: %v", run)
	}
	if g := capItem(t, w, "Steps goal"); g["detail"] != "6,240 of 8,000" || g["done"] != nil {
		t.Fatalf("steps goal row: %v", g)
	}
	if g := capItem(t, w, "Resting heart rate goal"); g["detail"] != "52 bpm, at most 70 bpm" || g["done"] != true {
		t.Fatalf("heart goal row: %v", g)
	}
	raw, _ := json.Marshal(w)
	for _, s := range []string{"Vitamin", "Headache", "Cervical", "Bob"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("widget carries %s: %s", s, raw)
		}
	}
	// Bob has only his ride today
	bob := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil)
	if got := capItems(t, bob); len(got) != 1 || got[0]["title"] != "Bob's ride" || bob["line"] != "Bob's ride · 1h 00m" {
		t.Fatalf("bob's widget: %v", bob)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/widget", nil)
}

func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	aliceDay(a)
	d := today()
	line := map[string]any{"request_id": "req-0000000000000001", "text": "weight 82.4"}
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=run", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + d, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + d, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=run", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=run", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + d, nil, 403},
		// Health keeps no quick lines: there is no such route, for anybody
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", line, 404},
		{"pcx_alice_widget", "POST", "/api/v1/pocket/capture", line, 404},
		{a.alice, "POST", "/api/v1/pocket/capture", line, 404},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/auth/me", nil, 200},
		// the rest of the app
		{"pcx_alice_day", "GET", "/api/v1/day/" + d, nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/home", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/dashboard", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/goals", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/workouts", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/metrics/weight", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/medications", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/symptoms", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/samples", map[string]any{"metric": "weight", "value": 80}, 403},
		{"pcx_alice_widget", "POST", "/api/v1/ingest", map[string]any{}, 403},
		{"pcx_alice_search", "GET", "/api/v1/export.json", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/settings", nil, 403},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=run", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + d, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", line, 404},
		{"pcx_alice_read", "GET", "/api/v1/day/" + d, nil, 200},
		{"pcx_alice_read", "GET", "/api/v1/workouts", nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only, and the private things stay out
	if got := capItems(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=run", nil)); len(got) != 2 || got[0]["title"] != "Morning run" {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := capItems(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=headache", nil)); len(got) != 0 {
		t.Fatalf("a symptom through the link: %v", got)
	}
	if w := a.ok("pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil); w["line"] != "6,240 of 8,000 steps" {
		t.Fatalf("alice's widget through the link: %v", w)
	}
	day := a.ok("pcx_alice_day", "GET", "/api/v1/pocket/day/"+d, nil)
	if !strings.HasPrefix(day["summary"].(string), "6240 steps") {
		t.Fatalf("alice's day through the link: %v", day)
	}
	capItem(t, day, "Morning run")
	capItem(t, day, "Slept 7h 20m")
	if got := capItems(t, a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=run", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's runs: %v", got)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); strings.Contains(w["line"].(string), "steps") {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if day := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+d, nil); len(capItems(t, day)) != 1 {
		t.Fatalf("bob's day through the link: %v", day)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), false},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/day/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/home"), false},
		{core.ScopeWidget, req("POST", "/api/v1/pocket/widget"), false},
		{core.ScopeDay, req("GET", "/api/v1/medications/day/2026-01-02"), false},
		{"health:read", req("GET", "/api/v1/pocket/search?q=x"), false},
		{"health:read", req("GET", "/api/v1/pocket/widget"), false},
		{"health:write", req("GET", "/api/v1/pocket/day/2026-01-02"), false},
		{"health:write", req("POST", "/api/v1/pocket/capture"), false},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Health tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "health" || m["category"] != "Health" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || day["label"] != "Health" || day["one"] != "workout" || day["many"] != "workouts" || widget == nil || widget["title"] != "Health today" {
		t.Fatalf("capabilities: %v %v %v", search, day, widget)
	}
	if m["capture"] != nil {
		t.Fatalf("Health keeps no quick lines: %v", m["capture"])
	}
	if bangs := search["bangs"].([]any); len(bangs) != 1 || bangs[0] != "health" {
		t.Fatalf("search: %v", search)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"health:read", "health:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("pocket:capture is offered")
	}
}
