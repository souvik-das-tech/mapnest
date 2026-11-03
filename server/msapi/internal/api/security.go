package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// --- login rate limiting ---------------------------------------------------

// loginLimiter counts failed sign-ins per key ("user:<name>", "ip:<addr>").
// max failures within window lock the key for lockout. In memory: a restart
// clears it, which is acceptable for a single admin API.
type loginLimiter struct {
	mu      sync.Mutex
	max     int
	lockout time.Duration
	now     func() time.Time
	entries map[string]*attempts
}

type attempts struct {
	count       int
	first       time.Time
	lockedUntil time.Time
}

// maxLimiterEntries bounds memory; older entries are swept first.
const maxLimiterEntries = 10000

func newLoginLimiter(max int, lockout time.Duration) *loginLimiter {
	if max < 1 {
		max = 5
	}
	if lockout <= 0 {
		lockout = 15 * time.Minute
	}
	return &loginLimiter{max: max, lockout: lockout, now: time.Now, entries: map[string]*attempts{}}
}

// blocked returns how long the longest lock among keys still lasts (0 = free).
func (l *loginLimiter) blocked(keys ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var wait time.Duration
	for _, k := range keys {
		if a := l.entries[k]; a != nil && now.Before(a.lockedUntil) {
			wait = max(wait, a.lockedUntil.Sub(now))
		}
	}
	return wait
}

// fail records a failed attempt for every key and reports whether any key
// is now locked.
func (l *loginLimiter) fail(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.entries) >= maxLimiterEntries {
		l.sweep(now)
	}
	locked := false
	for _, k := range keys {
		a := l.entries[k]
		if a == nil {
			if len(l.entries) >= maxLimiterEntries {
				continue // full of active entries: the other key still counts
			}
			a = &attempts{}
			l.entries[k] = a
		}
		if now.Sub(a.first) > l.lockout {
			a.count, a.first = 0, now
		}
		a.count++
		if a.count >= l.max {
			a.lockedUntil = now.Add(l.lockout)
			a.count, a.first = 0, now
			locked = true
		}
	}
	return locked
}

// reset forgets the keys after a successful sign-in.
func (l *loginLimiter) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.entries, k)
	}
}

func (l *loginLimiter) sweep(now time.Time) {
	for k, a := range l.entries {
		if !now.Before(a.lockedUntil) && now.Sub(a.first) > l.lockout {
			delete(l.entries, k)
		}
	}
}

// --- client IP ---------------------------------------------------------------

// clientIP is the caller's address. X-Real-IP and X-Forwarded-For are only
// believed when the direct peer is a trusted proxy (MSAPI_TRUSTED_PROXIES);
// in X-Forwarded-For the right-most untrusted hop wins, so a client cannot
// spoof its address by sending the header itself.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !s.trustedProxy(peer) {
		return peer.String()
	}
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			a = a.Unmap()
			if !s.trustedProxy(a) || i == 0 {
				return a.String()
			}
		}
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return a.Unmap().String()
	}
	return peer.String()
}

func (s *Server) trustedProxy(a netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// --- request context and audit log ------------------------------------------

type reqInfoKey struct{}

// reqInfo is filled in while a request runs and read by logRequests.
type reqInfo struct {
	actor string
	ip    string
}

func infoFrom(ctx context.Context) *reqInfo {
	if ri, ok := ctx.Value(reqInfoKey{}).(*reqInfo); ok {
		return ri
	}
	return &reqInfo{actor: actorNone}
}

// audit writes a security event. Every entry carries audit=true, so it can
// be filtered from the JSON logs (docker logs msapi | grep '"audit":true').
func (s *Server) audit(r *http.Request, event string, attrs ...any) {
	ri := infoFrom(r.Context())
	base := []any{"audit", true, "event", event, "actor", ri.actor, "ip", ri.ip,
		"method", r.Method, "path", r.URL.Path}
	s.log.Info("audit", append(base, attrs...)...)
}

// --- security headers ----------------------------------------------------------

// apiCSP: API responses are JSON, images or files and never need to run
// scripts or be framed. The Swagger page sets its own policy.
const apiCSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// securityHeaders sets the headers a scanner expects on every response.
// Handlers may override Cache-Control (icons) and Content-Security-Policy.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Content-Security-Policy", apiCSP)
		h.Set("Cache-Control", "no-store")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
