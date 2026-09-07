package webtest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// The client reads a problem the way the web package writes one, sends a
// JSON body with the precondition IfMatch quotes, and Decode reads the
// common response.
func TestClient_ProblemsAndDecode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := web.IfMatch(r); err != nil {
			_ = web.Problem{Status: http.StatusPreconditionRequired, Detail: "If-Match required"}.Write(w)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "name": body["name"]})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := webtest.NewClient(srv.URL)

	p := c.Put(t, "/things/1", map[string]string{"name": "x"}).Problem(t, http.StatusPreconditionRequired)
	if p.Detail != "If-Match required" {
		t.Errorf("problem detail = %q", p.Detail)
	}
	got := webtest.Decode[map[string]string](t, c.Put(t, "/things/1", map[string]string{"name": "x"}, webtest.IfMatch(3)), http.StatusOK)
	if got["id"] != "1" || got["name"] != "x" {
		t.Errorf("decoded = %v", got)
	}
	if res := c.Get(t, "/missing"); res.Status != http.StatusNotFound {
		t.Errorf("GET /missing = %d", res.Status)
	}
}

// Live is true only for a server answering the liveness probe with 200.
func TestLive_ObservesTheProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET "+web.HealthPath, web.Liveness())
	srv := httptest.NewServer(mux)
	if !webtest.Live(srv.URL) {
		t.Error("Live = false against a serving probe")
	}
	if webtest.Live(httptest.NewServer(http.NotFoundHandler()).URL) {
		t.Error("Live = true against a 404")
	}
	srv.Close()
	if webtest.Live(srv.URL) {
		t.Error("Live = true against a closed server")
	}
}

// Probe serves one GET through a handler into a recorder.
func TestProbe_RecordsOneGet(t *testing.T) {
	rec := webtest.Probe(web.Liveness(), web.HealthPath)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d", rec.Code)
	}
}
