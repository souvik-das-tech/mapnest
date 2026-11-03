package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"msapi/internal/config"
)

func doReq(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:4000"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func loginToken(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := doReq(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"mapserver"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("token: %v %s", err, rec.Body)
	}
	return resp.Token
}

func bearerHdr(tok string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + tok}
}

func TestLogoutRevokesToken(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	tok := loginToken(t, h)
	if c := doReq(h, http.MethodGet, "/api/v1/stores", "", bearerHdr(tok)).Code; c != http.StatusOK {
		t.Fatalf("before logout: %d", c)
	}
	if c := doReq(h, http.MethodPost, "/api/v1/auth/logout", "", bearerHdr(tok)).Code; c != http.StatusNoContent {
		t.Fatalf("logout: %d", c)
	}
	if c := doReq(h, http.MethodGet, "/api/v1/stores", "", bearerHdr(tok)).Code; c != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", c)
	}
}

func TestRevokeAllSessions(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	a, b := loginToken(t, h), loginToken(t, h)
	rec := doReq(h, http.MethodDelete, "/api/v1/auth/sessions", "", bearerHdr(a))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revoked":2`) {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	if c := doReq(h, http.MethodGet, "/api/v1/stores", "", bearerHdr(b)).Code; c != http.StatusUnauthorized {
		t.Fatalf("other session still valid: %d", c)
	}
}

func TestSessionPasswordChangeInvalidates(t *testing.T) {
	key := make([]byte, 32)
	before := &Server{cfg: config.Config{SecretKey: key, AdminPassword: "first-password-123"}}
	after := &Server{cfg: config.Config{SecretKey: key, AdminPassword: "another-password-123"}}
	tok := before.signSession(make([]byte, sessionIDBytes))
	if _, ok := before.parseToken(tok); !ok {
		t.Fatal("token rejected by its own signer")
	}
	if _, ok := after.parseToken(tok); ok {
		t.Fatal("token signed with the old password accepted")
	}
}

func TestLoginLockout(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/") // 3 attempts, 1 minute lockout
	for i := 0; i < 3; i++ {
		if c := doReq(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"bad"}`, nil).Code; c != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, c)
		}
	}
	rec := doReq(h, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"mapserver"}`, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked login: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestLimiterWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLoginLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	l.fail("k")
	now = now.Add(2 * time.Minute) // window passed: count restarts
	if l.fail("k") {
		t.Fatal("locked after window reset")
	}
	if !l.fail("k") || l.blocked("k") == 0 {
		t.Fatal("not locked after 2 failures")
	}
	now = now.Add(61 * time.Second)
	if l.blocked("k") != 0 {
		t.Fatal("still locked after lockout")
	}
}

func TestIdleTimeout(t *testing.T) {
	h := newTestServerRasters(t, "http://127.0.0.1:1/", t.TempDir(), func(c *config.Config) { c.SessionIdle = time.Nanosecond })
	tok := loginToken(t, h)
	// last_seen_at is stored in whole seconds; wait past one.
	time.Sleep(1100 * time.Millisecond)
	if c := doReq(h, http.MethodGet, "/api/v1/stores", "", bearerHdr(tok)).Code; c != http.StatusUnauthorized {
		t.Fatalf("idle session accepted: %d", c)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	rec := doReq(h, http.MethodGet, "/api/v1/stores", "", map[string]string{"X-API-Key": testKey})
	for k, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": apiCSP,
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestDocsPrivateByDefault(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/")
	if c := doReq(h, http.MethodGet, "/api/v1/docs", "", nil).Code; c != http.StatusUnauthorized {
		t.Errorf("anonymous docs: %d", c)
	}
	if c := doReq(h, http.MethodGet, "/api/v1/docs", "", map[string]string{"X-API-Key": testKey}).Code; c != http.StatusOK {
		t.Errorf("docs with key: %d", c)
	}
	pub := newTestServerRasters(t, "http://127.0.0.1:1/", t.TempDir(), func(c *config.Config) { c.PublicDocs = true })
	if c := doReq(pub, http.MethodGet, "/api/v1/swagger", "", nil).Code; c != http.StatusOK {
		t.Errorf("public swagger: %d", c)
	}
}

func TestHealthHidesDetailsFromAnonymous(t *testing.T) {
	h := newTestServer(t, "http://127.0.0.1:1/") // MapServer down
	rec := doReq(h, http.MethodGet, "/api/v1/health", "", nil)
	if strings.Contains(rec.Body.String(), "127.0.0.1") || strings.Contains(rec.Body.String(), `"version"`) {
		t.Errorf("anonymous health leaks details: %s", rec.Body)
	}
	rec = doReq(h, http.MethodGet, "/api/v1/health?stores=false", "", map[string]string{"X-API-Key": testKey})
	if !strings.Contains(rec.Body.String(), `"version"`) {
		t.Errorf("authenticated health lacks version: %s", rec.Body)
	}
}

func TestClientIP(t *testing.T) {
	trusted, _ := config.ParsePrefixes("10.0.0.0/8")
	s := &Server{cfg: config.Config{TrustedProxies: trusted}}
	// Untrusted peer: headers ignored. Trusted proxy: right-most untrusted
	// X-Forwarded-For hop (a client-sent hop is ignored), else X-Real-IP.
	cases := []struct {
		remote, xff, realIP, want string
	}{
		{"203.0.113.9:1", "1.2.3.4", "", "203.0.113.9"},
		{"10.0.0.5:1", "1.2.3.4", "", "1.2.3.4"},
		{"10.0.0.5:1", "6.6.6.6, 1.2.3.4", "", "1.2.3.4"},
		{"10.0.0.5:1", "1.2.3.4, 10.0.0.9", "", "1.2.3.4"},
		{"10.0.0.5:1", "", "5.6.7.8", "5.6.7.8"},
		{"[::ffff:10.0.0.5]:1", "1.2.3.4", "", "1.2.3.4"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.realIP != "" {
			req.Header.Set("X-Real-IP", c.realIP)
		}
		if got := s.clientIP(req); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}
