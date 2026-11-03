package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-API-Key", testKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStoresValidation(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")

	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"unknown field", `{"name":"a","type":"s3","nope":1}`, http.StatusBadRequest},
		{"bad name", `{"name":"1bad","type":"postgis","connection":{}}`, http.StatusUnprocessableEntity},
		{"bad type", `{"name":"a","type":"oracle","connection":{}}`, http.StatusUnprocessableEntity},
		{"missing pg fields", `{"name":"a","type":"postgis","connection":{"host":"x"}}`, http.StatusUnprocessableEntity},
		{"missing s3 secret", `{"name":"a","type":"s3","connection":{"bucket":"b","access_key":"k"}}`, http.StatusUnprocessableEntity},
		{"unknown connection field", `{"name":"a","type":"s3","connection":{"bucket":"b","x":1}}`, http.StatusUnprocessableEntity},
		{"local path escapes", `{"name":"a","type":"local","connection":{"path":"../etc"}}`, http.StatusUnprocessableEntity},
		{"local folder missing", `{"name":"a","type":"local","connection":{"path":"nope"}}`, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, "/api/v1/stores", c.body)
			if rec.Code != c.want {
				t.Errorf("got %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestStoresConnectionTestFails(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	// Nothing listens on port 1, so the connection test must fail and
	// nothing must be saved.
	body := `{"name":"db","type":"postgis","connection":{"host":"127.0.0.1","port":1,"database":"x","user":"u","password":"p"}}`
	rec := do(t, h, http.MethodPost, "/api/v1/stores", body)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "connection test failed") {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	rec = do(t, h, http.MethodGet, "/api/v1/stores", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
}

func TestStoresNotFound(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/stores/nope"},
		{http.MethodDelete, "/api/v1/stores/nope"},
		{http.MethodPost, "/api/v1/stores/nope/test"},
		{http.MethodGet, "/api/v1/stores/nope/resources"},
	} {
		if rec := do(t, h, c.method, c.path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: got %d", c.method, c.path, rec.Code)
		}
	}
}

func TestLocalStore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "drone", "2026"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "drone", "2026", "ortho.tif"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTestServerRasters(t, "http://127.0.0.1:1/", root)

	rec := do(t, h, http.MethodPost, "/api/v1/stores", `{"name":"drone","type":"local","connection":{"path":"/drone/"}}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"path":"drone"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, http.MethodGet, "/api/v1/stores/drone/resources", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"path":"2026/ortho.tif"`) {
		t.Fatalf("resources: %d %s", rec.Code, rec.Body)
	}
	// Paths are relative to the store folder and may not leave it.
	rec = do(t, h, http.MethodPost, "/api/v1/layers/inspect", `{"store":"drone","path":"../drone/2026/ortho.tif"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("inspect ..: %d %s", rec.Code, rec.Body)
	}
}
