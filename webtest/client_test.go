package webtest_test

import (
	"bytes"
	"encoding/json"
	"io"
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
			_ = web.Problem{
				Status: http.StatusPreconditionRequired,
				Detail: "If-Match required",
				Extras: map[string]any{"header": "If-Match"},
			}.Write(w)
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
	if p.Extras["header"] != "If-Match" {
		t.Errorf("problem extras = %v, want header=If-Match", p.Extras)
	}
	got := webtest.Decode[map[string]string](t, c.Put(t, "/things/1", map[string]string{"name": "x"}, webtest.IfMatch(3)), http.StatusOK)
	if got["id"] != "1" || got["name"] != "x" {
		t.Errorf("decoded = %v", got)
	}
	if res := c.Get(t, "/missing"); res.Status != http.StatusNotFound {
		t.Errorf("GET /missing = %d", res.Status)
	}
}

// A Raw body travels under its own media type with its length, the upload
// ReadUpload accepts; one with no media type is refused; and the proxied
// bytes and their headers come back whole.
func TestClient_RawUploadAndObjectRead(t *testing.T) {
	var stored []byte
	var storedType string
	mux := http.NewServeMux()
	ew := web.NewErrorWriter()
	mux.Handle("PUT /files/{name}", web.Handle(func(w http.ResponseWriter, r *http.Request) error {
		u, err := web.ReadUpload(w, r, 1<<10)
		if err != nil {
			return err
		}
		stored, err = io.ReadAll(u.Body)
		storedType = u.ContentType
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusCreated)
		return nil
	}, ew))
	mux.Handle("GET /files/{name}", web.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return web.WriteObject(w, r, web.Object{ContentType: storedType, Size: int64(len(stored)), ETag: `"1"`}, bytes.NewReader(stored))
	}, ew))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := webtest.NewClient(srv.URL)

	png := []byte{0x89, 'P', 'N', 'G'}
	c.Put(t, "/files/logo.png", webtest.Raw{ContentType: "image/png", Body: png}).Expect(t, http.StatusCreated)
	_ = c.Put(t, "/files/logo.png", webtest.Raw{Body: png}).Problem(t, http.StatusUnsupportedMediaType)

	res := c.Get(t, "/files/logo.png").Expect(t, http.StatusOK)
	if !bytes.Equal(res.Body, png) {
		t.Errorf("body = %v, want %v", res.Body, png)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	c.Get(t, "/files/logo.png", webtest.Header{Name: "If-None-Match", Value: `"1"`}).Expect(t, http.StatusNotModified)
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
