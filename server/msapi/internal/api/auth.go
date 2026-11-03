package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"msapi/internal/store"
)

// Admin UI sign-in (SECURITY.md, "Authentication").
//
// POST /auth/login checks the username and password from the config and
// returns a session token for "Authorization: Bearer". Scripts use the
// X-API-Key header instead.
//
// Token: base64url(32 random bytes) + "." + base64url(HMAC-SHA256(id)).
// The session row (stored by the SHA-256 of the id, never the id itself)
// holds the absolute expiry and the last activity for the idle timeout, so
// sign-out and "sign out everywhere" take effect at once. The HMAC key mixes
// in the admin password: changing the password invalidates every token.
//
// Failed sign-ins are rate limited per username and per client IP
// (loginLimiter); a locked caller gets 429 with Retry-After.

const (
	sessionIDBytes = 32
	// touchEvery limits writes: last_seen_at is updated at most this often.
	touchEvery = time.Minute
	// failDelay slows down every failed attempt a little more.
	failDelay = 300 * time.Millisecond
	// maxUsernameLen bounds what is hashed, logged and kept in the limiter.
	maxUsernameLen = 128
)

func (s *Server) routeAuth(public, protected *http.ServeMux) {
	public.HandleFunc("POST "+basePath+"/auth/login", s.login)
	public.HandleFunc("POST "+basePath+"/auth/logout", s.logout)
	protected.HandleFunc("GET "+basePath+"/auth/session", s.sessionInfo)
	protected.HandleFunc("DELETE "+basePath+"/auth/sessions", s.revokeAllSessions)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token              string `json:"token"`
	Username           string `json:"username"`
	ExpiresAt          string `json:"expires_at"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user := strings.TrimSpace(req.Username)
	if len(user) > maxUsernameLen {
		user = user[:maxUsernameLen]
	}
	keys := []string{"user:" + strings.ToLower(user), "ip:" + ip}

	if wait := s.limiter.blocked(keys...); wait > 0 {
		s.audit(r, "login_blocked", "username", user, "retry_after_s", int(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests,
			fmt.Sprintf("too many failed sign-ins; try again in %d minute(s)", int(wait.Minutes())+1))
		return
	}

	// Compare fixed-length digests so neither the length nor the content of
	// the configured values leaks through timing.
	userOK := equalDigest(user, s.cfg.AdminUser)
	passOK := equalDigest(req.Password, s.cfg.AdminPassword)
	if !userOK || !passOK {
		locked := s.limiter.fail(keys...)
		s.audit(r, "login_failed", "username", user, "locked", locked)
		time.Sleep(failDelay)
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	s.limiter.reset(keys...)

	now := time.Now()
	id := make([]byte, sessionIDBytes)
	if _, err := rand.Read(id); err != nil {
		s.internalError(w, err)
		return
	}
	ss := store.Session{
		IDHash:     hashID(id),
		Username:   s.cfg.AdminUser,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.cfg.SessionTTL),
		LastSeenAt: now,
		ClientIP:   ip,
		UserAgent:  r.UserAgent(),
	}
	_ = s.store.PurgeSessions(r.Context(), now, s.cfg.SessionIdle)
	if err := s.store.CreateSession(r.Context(), ss); err != nil {
		s.internalError(w, err)
		return
	}
	s.audit(r, "login_success", "username", user)
	writeJSON(w, http.StatusOK, loginResponse{
		Token:              s.signSession(id),
		Username:           s.cfg.AdminUser,
		ExpiresAt:          ss.ExpiresAt.UTC().Format(time.RFC3339),
		IdleTimeoutSeconds: int(s.cfg.SessionIdle.Seconds()),
	})
}

// logout ends the caller's session. It always answers 204, so it can be
// called with an expired or unknown token.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.parseToken(bearer(r)); ok {
		if err := s.store.DeleteSession(r.Context(), hashID(id)); err != nil {
			s.internalError(w, err)
			return
		}
		s.audit(r, "logout")
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionInfoResponse struct {
	Username           string `json:"username"`
	Method             string `json:"method"` // "session" or "api_key"
	ExpiresAt          string `json:"expires_at,omitempty"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds,omitempty"`
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") != "" {
		writeJSON(w, http.StatusOK, sessionInfoResponse{Username: actorAPIKey, Method: "api_key"})
		return
	}
	id, _ := s.parseToken(bearer(r))
	ss, ok, err := s.store.GetSession(r.Context(), hashID(id))
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	writeJSON(w, http.StatusOK, sessionInfoResponse{
		Username:           ss.Username,
		Method:             "session",
		ExpiresAt:          ss.ExpiresAt.UTC().Format(time.RFC3339),
		IdleTimeoutSeconds: int(s.cfg.SessionIdle.Seconds()),
	})
}

// revokeAllSessions signs every admin UI session out, including the caller's.
func (s *Server) revokeAllSessions(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteAllSessions(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.audit(r, "sessions_revoked", "count", n)
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}

func (s *Server) sessionKey() []byte {
	key := sha256.Sum256(append(append([]byte("msapi-session-v2|"), s.cfg.SecretKey...), s.cfg.AdminPassword...))
	return key[:]
}

func (s *Server) signSession(id []byte) string {
	m := hmac.New(sha256.New, s.sessionKey())
	m.Write(id)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(id) + "." + enc.EncodeToString(m.Sum(nil))
}

// parseToken checks the token's signature and returns the session id.
func (s *Server) parseToken(token string) ([]byte, bool) {
	p, sig, ok := strings.Cut(token, ".")
	if !ok || len(token) > 256 {
		return nil, false
	}
	enc := base64.RawURLEncoding
	id, err := enc.DecodeString(p)
	if err != nil || len(id) != sessionIDBytes {
		return nil, false
	}
	mac, err := enc.DecodeString(sig)
	if err != nil {
		return nil, false
	}
	m := hmac.New(sha256.New, s.sessionKey())
	m.Write(id)
	return id, hmac.Equal(mac, m.Sum(nil))
}

// validSession checks a bearer token against the session table: present,
// not expired, not idle. touch=false is for background polling (/health),
// which must not keep an unattended session alive.
func (s *Server) validSession(ctx context.Context, token string, touch bool) bool {
	id, ok := s.parseToken(token)
	if !ok {
		return false
	}
	h := hashID(id)
	ss, found, err := s.store.GetSession(ctx, h)
	if err != nil {
		s.log.Error("session lookup", "error", err)
		return false
	}
	if !found || ss.Username != s.cfg.AdminUser {
		return false
	}
	now := time.Now()
	if !now.Before(ss.ExpiresAt) || now.Sub(ss.LastSeenAt) > s.cfg.SessionIdle {
		_ = s.store.DeleteSession(ctx, h)
		return false
	}
	if touch && now.Sub(ss.LastSeenAt) >= touchEvery {
		if err := s.store.TouchSession(ctx, h, now); err != nil {
			s.log.Warn("session touch", "error", err)
		}
	}
	return true
}

const (
	actorAPIKey = "api-key"
	actorNone   = "anonymous"
)

// authenticate accepts the API key (scripts) or a session token (admin UI)
// and names the caller for the audit log.
func (s *Server) authenticate(r *http.Request, touch bool) (actor string, ok bool) {
	if k := r.Header.Get("X-API-Key"); k != "" {
		if equalDigest(k, s.cfg.APIKey) {
			return actorAPIKey, true
		}
		return actorNone, false
	}
	if t := bearer(r); t != "" && s.validSession(r.Context(), t, touch) {
		return s.cfg.AdminUser, true
	}
	return actorNone, false
}

// authorized is authenticate for read-only checks that must not extend the
// session (used by /health).
func (s *Server) authorized(r *http.Request) bool {
	_, ok := s.authenticate(r, false)
	return ok
}

func bearer(r *http.Request) string {
	t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(t)
}

func hashID(id []byte) string {
	sum := sha256.Sum256(id)
	return hex.EncodeToString(sum[:])
}

func equalDigest(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
