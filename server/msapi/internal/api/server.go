// Package api holds the HTTP handlers for /api/v1 (PLAN.md §7).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"msapi/internal/config"
	"msapi/internal/crypto"
	"msapi/internal/pgread"
	"msapi/internal/store"
)

const (
	basePath     = "/api/v1"
	maxJSONBytes = 1 << 20
)

type Server struct {
	cfg     config.Config
	store   *store.Store
	box     *crypto.Box
	pools   *pgread.Pools
	log     *slog.Logger
	http    *http.Client // for calls to MapServer
	version string

	// writeMu serializes everything that writes files MapServer reads
	// (pg_service.conf, services.map, symbols). PLAN.md §3 rule 4.
	writeMu sync.Mutex

	fontsOnce sync.Once
	fontset   bool // fonts/fontset.txt written (labels available)

	cache *rasterCache // Cloud Optimized copies of non-COG rasters

	limiter *loginLimiter // failed sign-ins per username and client IP
}

func New(cfg config.Config, st *store.Store, log *slog.Logger, version string) (*Server, error) {
	box, err := crypto.New(cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return &Server{
		cfg:     cfg,
		store:   st,
		box:     box,
		pools:   pgread.NewPools(),
		log:     log,
		http:    &http.Client{Timeout: 5 * time.Second},
		version: version,
		cache:   newRasterCache(),
		limiter: newLoginLimiter(cfg.LoginMaxAttempts, cfg.LoginLockout),
	}, nil
}

// Close releases pooled connections to user databases.
func (s *Server) Close() { s.pools.Close() }

// Handler returns the full router with middleware applied.
func (s *Server) Handler() http.Handler {
	// Everything under /api/v1 except /health, /auth/login and /auth/logout
	// needs the API key or a session token. /docs and /swagger are public
	// only with MSAPI_PUBLIC_DOCS=on.
	protected := http.NewServeMux()
	root := http.NewServeMux()
	s.routeWorkspaces(protected)
	s.routeStores(protected)
	s.routeLayers(protected)
	s.routeAttributes(protected)
	s.routeStyles(protected)
	s.routeIcons(protected)
	s.routeMapfile(protected)
	s.routeSystem(protected)
	s.routeAuth(root, protected)
	docs := protected
	if s.cfg.PublicDocs {
		docs = root
	}
	docs.HandleFunc("GET "+basePath+"/docs", s.handleDocs)
	docs.HandleFunc("GET "+basePath+"/swagger", s.handleSwagger)
	protected.HandleFunc("/", notFound)

	root.HandleFunc("GET "+basePath+"/health", s.handleHealth)
	root.Handle(basePath+"/", s.requireAPIKey(protected))
	root.HandleFunc("/", notFound)

	return s.withRequestInfo(s.recoverPanics(securityHeaders(s.logRequests(root))))
}

// withRequestInfo puts the client IP (and later the actor) in the context
// for the request and audit logs.
func (s *Server) withRequestInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ri := &reqInfo{actor: actorNone, ip: s.clientIP(r)}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, ri)))
	})
}

func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.authenticate(r, true)
		if !ok {
			if r.Header.Get("X-API-Key") != "" {
				s.audit(r, "api_key_rejected")
			}
			writeError(w, http.StatusUnauthorized, "sign in again (or send a valid X-API-Key)")
			return
		}
		infoFrom(r.Context()).actor = actor
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer (Flush etc.).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests writes one line per request with the caller and client IP.
// Changes (anything but GET/HEAD/OPTIONS) are marked audit=true, so the
// audit trail is: docker logs msapi | grep '"audit":true'.
// Query strings are not logged.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		ri := infoFrom(r.Context())
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"actor", ri.actor,
			"ip", ri.ip,
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			attrs = append(attrs, "audit", true, "event", "change")
		}
		s.log.Info("request", attrs...)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "path", r.URL.Path, "error", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

// internalError logs the real error and returns a generic 500.
func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("internal error", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// decodeJSON reads a JSON body (max 1 MB, unknown fields rejected) and
// writes the error response itself. Returns false if the handler should stop.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	if err := strictUnmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON", err.Error())
		return false
	}
	return true
}

func strictUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after JSON value")
	}
	return nil
}

type errorBody struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string, details ...string) {
	writeJSON(w, status, errorBody{Error: msg, Details: details})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
