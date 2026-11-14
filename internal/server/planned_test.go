package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A planned interval session sent at the watch's own rate: the plan's name and
// id, each step's target beside what was done, the series as recorded or in
// 5-second buckets.
func TestPlannedWorkoutTargetsAndNativeSeries(t *testing.T) {
	reverseGeocodeURL = ""
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"hr_zones": []float64{120, 140, 155, 170}})
	start := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	// one reading a second, 0.3 s after the full second: 300 s easy at 2.5 m/s (128 bpm),
	// a 20 s stride at 5 m/s (165 bpm, its first 4 s still at 150), 100 s walk at 1.5 m/s
	hr, speed, dist := [][2]float64{}, [][2]float64{}, [][2]float64{{0, 0}}
	total := 0.0
	for s := 0; s < 420; s++ {
		v, b := 2.5, 128.0
		switch {
		case s >= 300 && s < 320:
			v, b = 5, 165
			if s < 304 {
				b = 150
			}
		case s >= 320:
			v, b = 1.5, 110
		}
		tt := float64(s) + 0.3
		total += v
		hr = append(hr, [2]float64{tt, b})
		speed = append(speed, [2]float64{tt, v})
		dist = append(dist, [2]float64{tt + 1, total})
	}
	segs := []map[string]any{
		{"kind": "interval", "purpose": "warmup", "label": "Warm-up", "goal": "5 min", "start_s": 0, "end_s": 300,
			"target": map[string]any{"type": "heart_rate_zone", "zone": 2}},
		{"kind": "interval", "purpose": "work", "label": "Stride 1", "goal": "20 s", "start_s": 300, "end_s": 320,
			"target": map[string]any{"type": "pace", "min": 190, "max": 210}},
		{"kind": "interval", "purpose": "recovery", "label": "Recovery 1", "start_s": 320, "end_s": 420,
			"target": map[string]any{"type": "heart_rate", "max": 105, "metric": "average"}},
	}
	w := map[string]any{"name": "Running", "plan_name": "Strides 6×20 s", "plan_id": "7C1F-PLAN", "series_step": 0,
		"start": start.Format(time.RFC3339), "end": start.Add(420 * time.Second).Format(time.RFC3339), "distance_m": total,
		"series": map[string]any{"heart_rate": hr, "speed": speed, "distance": dist}, "segments": segs}
	a.ok(a.alice, "POST", "/api/v1/ingest?source=pocket-homelab", map[string]any{"workouts": []any{w}})

	_, _, list := a.call(a.alice, "GET", "/api/v1/workouts?plan_id=7C1F-PLAN", nil)
	if len(list) != 1 {
		t.Fatalf("by plan = %v", list)
	}
	wk := list[0].(map[string]any)
	if wk["name"] != "Strides 6×20 s" || wk["plan_name"] != "Strides 6×20 s" || wk["plan_id"] != "7C1F-PLAN" || wk["kind"] != "running" {
		t.Fatalf("workout = %v", wk)
	}
	id := int64(num(wk, "id"))

	tr := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/workouts/%d/track", id), nil)
	if num(tr, "series_step") != 0 || num(tr, "recorded_step") != 0 || len(tr["series"].(map[string]any)["heart_rate"].([]any)) != 420 {
		t.Fatalf("native series: step %v, %d readings", tr["series_step"], len(tr["series"].(map[string]any)["heart_rate"].([]any)))
	}
	parts := tr["segments"].([]any)
	target := func(i int) map[string]any { return parts[i].(map[string]any)["target"].(map[string]any) }
	warm, stride, rec := target(0), target(1), target(2)
	// zone 2 = 120–140 bpm from the settings; 128 bpm all along
	if warm["status"] != "in" || num(warm, "range_min") != 120 || num(warm, "range_max") != 140 || num(warm, "in_pct") != 100 || warm["zones_from"] != "settings" {
		t.Fatalf("warm-up target = %v", warm)
	}
	// pace 3:10–3:30 /km = 4.76–5.26 m/s; 5 m/s for the 20 s
	if stride["type"] != "speed" || stride["status"] != "in" || num(stride, "actual") < 4.9 || num(stride, "actual") > 5.1 || num(stride, "in_pct") < 95 {
		t.Fatalf("stride target = %v", stride)
	}
	if hr := num(parts[1].(map[string]any), "hr_avg"); hr < 161 || hr > 163 {
		t.Fatalf("stride heart rate = %v (4 s at 150, 16 s at 165)", hr)
	}
	// average heart rate under 105 asked, 110 done
	if rec["status"] != "above" || rec["metric"] != "average" || num(rec, "above_pct") < 95 {
		t.Fatalf("recovery target = %v", rec)
	}

	five := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/workouts/%d/track?step=5", id), nil)
	if num(five, "series_step") != 5 || num(five, "recorded_step") != 0 || len(five["series"].(map[string]any)["heart_rate"].([]any)) != 84 {
		t.Fatalf("5 s series: %v, %d readings", five["series_step"], len(five["series"].(map[string]any)["heart_rate"].([]any)))
	}
	// the parts are the same either way
	if num(five["segments"].([]any)[1].(map[string]any)["target"].(map[string]any), "in_pct") != num(stride, "in_pct") {
		t.Fatal("parts changed with the step")
	}

	// an older phone sends it again without the plan: the name stays
	delete(w, "plan_name")
	delete(w, "plan_id")
	a.ok(a.alice, "POST", "/api/v1/ingest?source=pocket-homelab", map[string]any{"workouts": []any{w}})
	got := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/workouts/%d", id), nil)
	if got["name"] != "Strides 6×20 s" || got["plan_id"] != "7C1F-PLAN" {
		t.Fatalf("after a resend without the plan: %v", got)
	}
}

// Series sent every 5 s by an older phone are recognised as such.
func TestSeriesStepWorkedOut(t *testing.T) {
	a := newApp(t, nil)
	d := today()
	w := map[string]any{"name": "Cycling", "start": d + "T07:00:00Z", "end": d + "T07:01:00Z",
		"series": map[string]any{"heart_rate": [][2]float64{{0, 100}, {5, 110}, {15, 120}}}}
	a.ok(a.alice, "POST", "/api/v1/ingest", map[string]any{"workouts": []any{w}})
	_, _, list := a.call(a.alice, "GET", "/api/v1/workouts", nil)
	tr := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/workouts/%d/track", int64(num(list[0].(map[string]any), "id"))), nil)
	if num(tr, "recorded_step") != 5 {
		t.Fatalf("recorded_step = %v", tr["recorded_step"])
	}
}

// A new workout goes to the person's own webhook, signed, without Pocket Core.
func TestWorkoutWebhook(t *testing.T) {
	type hit struct {
		event, sig string
		body       []byte
	}
	got := make(chan hit, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- hit{r.Header.Get("X-Pocket-Event"), r.Header.Get("X-Pocket-Signature"), b}
	}))
	defer hook.Close()
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"webhook_url": hook.URL, "webhook_secret": "s3cret",
		"webhook_events": []string{"health.workout_recorded", "health.nope"}, "webhook_track": true})
	st := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if ev := st["webhook_events"].([]any); len(ev) != 1 || st["webhook_secret"] == "s3cret" {
		t.Fatalf("settings = %v %v", st["webhook_events"], st["webhook_secret"])
	}
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	w := map[string]any{"name": "Running", "plan_name": "Tempo", "start": start.Format(time.RFC3339), "end": start.Add(10 * time.Minute).Format(time.RFC3339),
		"distance_m": 2500, "series": map[string]any{"heart_rate": [][2]float64{{0, 140}, {1.5, 141}}},
		"segments": []map[string]any{{"kind": "interval", "purpose": "work", "start_s": 0, "end_s": 600, "target": map[string]any{"type": "heart_rate", "min": 135, "max": 150}}}}
	a.ok(a.alice, "POST", "/api/v1/ingest", map[string]any{"workouts": []any{w}})
	select {
	case h := <-got:
		m := hmac.New(sha256.New, []byte("s3cret"))
		m.Write(h.body)
		if h.event != "health.workout_recorded" || h.sig != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			t.Fatalf("event %q signature %q", h.event, h.sig)
		}
		var body struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		_ = json.Unmarshal(h.body, &body)
		track, _ := body.Data["track"].(map[string]any)
		if body.Data["plan_name"] != "Tempo" || body.Data["user_email"] != nil || track == nil || len(track["segments"].([]any)) != 1 {
			t.Fatalf("body = %s", h.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no webhook")
	}
	// the Test button
	r := a.ok(a.alice, "POST", "/api/v1/integrations/test", map[string]any{"which": "webhook"})
	if r["ok"] != true {
		t.Fatalf("test = %v", r)
	}
	if h := <-got; h.event != "health.test" {
		t.Fatalf("test event = %q", h.event)
	}
}
