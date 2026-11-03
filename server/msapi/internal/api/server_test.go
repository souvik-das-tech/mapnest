package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"msapi/internal/config"
	"msapi/internal/store"
)

const testKey = "test-key-0123456789"

func newTestServer(t *testing.T, mapserverURL string) http.Handler {
	t.Helper()
	return newTestServerRasters(t, mapserverURL, t.TempDir())
}

// newTestServerRasters uses rasterDir as MSAPI_RASTER_DIR.
func newTestServerRasters(t *testing.T, mapserverURL, rasterDir string, opts ...func(*config.Config)) http.Handler {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "msadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Config{
		APIKey:        testKey,
		SecretKey:     make([]byte, 32),
		MapServerURL:  mapserverURL,
		MapfileDir:    t.TempDir(),
		RasterDir:     rasterDir,
		MapServerGID:  -1,
		AdminUser:     "admin",
		AdminPassword: "mapserver",
		SessionTTL:    time.Hour,
		SessionIdle:   30 * time.Minute,

		LoginMaxAttempts: 3,
		LoginLockout:     time.Minute,
	}
	for _, f := range opts {
		f(&cfg)
	}
	srv, err := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv.Handler()
}

func get(t *testing.T, h http.Handler, path, key string) (*httptest.ResponseRecorder, healthResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body healthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestHealthOK(t *testing.T) {
	ms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<?xml version="1.0"?><WMS_Capabilities/>`)
	}))
	defer ms.Close()

	rec, body := get(t, newTestServer(t, ms.URL+"/"), "/api/v1/health", "")
	if rec.Code != http.StatusOK || body.Status != "ok" {
		t.Fatalf("got %d %s: %s", rec.Code, body.Status, rec.Body)
	}
	if !body.Checks["sqlite"].OK || !body.Checks["mapserver"].OK {
		t.Errorf("checks: %+v", body.Checks)
	}
}

func TestHealthMapServerDown(t *testing.T) {
	ms := httptest.NewServer(http.NotFoundHandler())
	ms.Close() // nothing listening

	rec, body := get(t, newTestServer(t, ms.URL+"/"), "/api/v1/health", "")
	if rec.Code != http.StatusServiceUnavailable || body.Status != "degraded" {
		t.Fatalf("got %d %s", rec.Code, body.Status)
	}
	if !body.Checks["sqlite"].OK {
		t.Error("sqlite should still be OK")
	}
	if body.Checks["mapserver"].OK || body.Checks["mapserver"].Error == "" {
		t.Errorf("mapserver check: %+v", body.Checks["mapserver"])
	}
}

func TestHealthMapfileError(t *testing.T) {
	ms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<ServiceExceptionReport>msLoadMap(): bad mapfile</ServiceExceptionReport>`)
	}))
	defer ms.Close()

	rec, body := get(t, newTestServer(t, ms.URL+"/"), "/api/v1/health", "")
	if rec.Code != http.StatusServiceUnavailable || body.Checks["mapserver"].OK {
		t.Fatalf("got %d %+v", rec.Code, body.Checks["mapserver"])
	}
}

func TestAPIKeyRequired(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	cases := []struct {
		key  string
		want int
	}{
		{"", http.StatusUnauthorized},
		{"wrong", http.StatusUnauthorized},
		{testKey, http.StatusNotFound}, // authenticated; no such route yet
	}
	for _, c := range cases {
		rec, _ := get(t, h, "/api/v1/nothing-here", c.key)
		if rec.Code != c.want {
			t.Errorf("key %q: got %d, want %d", c.key, rec.Code, c.want)
		}
	}
}

func TestLogin(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	withToken := func(tok string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stores", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if rec := post(`{"username":"admin","password":"nope"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	rec := post(`{"username":"admin","password":"mapserver"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("token: %v %s", err, rec.Body)
	}
	if c := withToken(resp.Token); c != http.StatusOK {
		t.Errorf("valid token: %d", c)
	}
	if c := withToken(resp.Token + "x"); c != http.StatusUnauthorized {
		t.Errorf("tampered token: %d", c)
	}
	if c := withToken("garbage"); c != http.StatusUnauthorized {
		t.Errorf("garbage token: %d", c)
	}
}
