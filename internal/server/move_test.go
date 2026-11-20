package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-health/internal/config"
)

// moveLinks starts Health behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "health:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"health","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/apple", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/metrics", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "POST", "/api/v1/import/csv", 403},
		{imp, "POST", "/api/v1/ingest", 403},
		{imp, "POST", "/api/v1/samples", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	// the Apple Health import is reachable for move:import (a bad upload, not a refusal)
	if code, body := a.raw(imp, "POST", "/api/v1/import/apple", []byte("{}")); code != 400 {
		t.Errorf("apple import with move:import → %d %s", code, body)
	}
	_, body := a.raw(exp, "GET", core.PortablePath, nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 2 || p.Exports[0].Kind != core.KindPocket || p.Imports[1].Form != "file" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's health data to Bob: the import adds once, leaves Bob's
// own goal alone, and keeps the two apart.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	a.ok(a.alice, "POST", "/api/v1/samples", map[string]any{"metric": "weight", "value": 71.5, "at": "2026-11-01T08:00:00Z"})
	a.ok(a.alice, "PUT", "/api/v1/goals/steps", map[string]any{"target": 12000})
	med := a.ok(a.alice, "POST", "/api/v1/medications", map[string]any{"name": "Alicemycin", "dose": "1 pill", "stock": 30})
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/medications/%d/log", int64(num(med, "id"))), map[string]any{"taken_at": "2026-11-01T09:00:00Z"})
	a.ok(a.alice, "POST", "/api/v1/symptoms", map[string]any{"name": "Headache", "severity": 2, "at": "2026-11-01T10:00:00Z"})
	a.ok(a.bob, "PUT", "/api/v1/goals/steps", map[string]any{"target": 5000})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", nil)
	if code != 200 || !strings.Contains(string(export), "Alicemycin") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", nil); strings.Contains(string(bob), "Alicemycin") || strings.Contains(string(bob), "71.5") {
		t.Fatalf("bob's export holds alice's data: %s", bob)
	}
	imp := func() map[string]any {
		code, out := a.raw("pcx_bob_import", "POST", "/api/v1/import", export)
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	first := imp()
	if num(first, "samples") != 1 || num(first, "medications") != 1 || num(first, "doses") != 1 || num(first, "symptoms") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := imp()
	total := 0.0
	for _, v := range second {
		total += v.(float64)
	}
	if total != 0 {
		t.Fatalf("second import added: %v", second)
	}
	_, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", nil)
	var doc exportDoc
	_ = json.Unmarshal(bob, &doc)
	if len(doc.Medications) != 1 || doc.Medications[0].Stock != 29 || len(doc.MedLog) != 1 || len(doc.Symptoms) != 1 {
		t.Fatalf("bob after the import: %+v", doc)
	}
	for _, g := range doc.Goals {
		if g.Metric == "steps" && g.Target != 5000 {
			t.Fatalf("bob's own steps goal changed: %+v", g)
		}
	}
	_, alice := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", nil)
	var ad exportDoc
	_ = json.Unmarshal(alice, &ad)
	if len(ad.Medications) != 1 || len(ad.MedLog) != 1 || len(ad.Symptoms) != 1 {
		t.Fatalf("alice changed: %+v", ad)
	}
}
