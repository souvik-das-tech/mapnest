package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type check struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type healthResponse struct {
	Status  string           `json:"status"` // "ok" or "degraded"
	Version string           `json:"version,omitempty"`
	Checks  map[string]check `json:"checks"`
	// Per-store reachability. Informational: a customer database being
	// down does not make msapi itself unhealthy.
	Stores map[string]check `json:"stores,omitempty"`
}

// handleHealth reports API + SQLite + MapServer (these decide 200 vs 503) and
// the status of each store. /health is public, so stores (names, hosts in
// errors) are only reported to callers with a valid X-API-Key;
// ?stores=false skips the store tests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		checks = map[string]check{}
	)
	run := func(name string, fn func(context.Context) (string, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			detail, err := fn(ctx)
			c := check{OK: err == nil, LatencyMS: time.Since(start).Milliseconds(), Detail: detail}
			if err != nil {
				c.Error = err.Error()
			}
			mu.Lock()
			checks[name] = c
			mu.Unlock()
		}()
	}
	run("sqlite", s.checkSQLite)
	run("mapserver", s.checkMapServer)

	var stores map[string]check
	if s.authorized(r) && r.URL.Query().Get("stores") != "false" {
		stores = map[string]check{}
		if rows, err := s.store.ListStores(ctx); err == nil {
			for _, row := range rows {
				wg.Add(1)
				go func() {
					defer wg.Done()
					start := time.Now()
					c := check{OK: true}
					conn, err := s.loadConn(row)
					if err == nil {
						_, err = conn.test(ctx)
					}
					c.LatencyMS = time.Since(start).Milliseconds()
					if err != nil {
						c.OK, c.Error = false, err.Error()
					}
					mu.Lock()
					stores[row.Name] = c
					mu.Unlock()
				}()
			}
		}
	}
	wg.Wait()

	resp := healthResponse{Status: "ok", Version: s.version, Checks: checks, Stores: stores}
	status := http.StatusOK
	for _, c := range checks {
		if !c.OK {
			resp.Status = "degraded"
			status = http.StatusServiceUnavailable
		}
	}
	// Anonymous callers (load balancers, uptime checks) get the verdict
	// only: no version and no error text that could reveal hosts or paths.
	if !s.authorized(r) {
		resp.Version = ""
		for name, c := range checks {
			c.Detail = ""
			if c.Error != "" {
				c.Error = "unavailable"
			}
			checks[name] = c
		}
	}
	writeJSON(w, status, resp)
}

func (s *Server) checkSQLite(ctx context.Context) (string, error) {
	if err := s.store.Ping(ctx); err != nil {
		return "", err
	}
	v, err := s.store.SchemaVersion(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("schema version %d", v), nil
}

// checkMapServer asks MapServer for WMS capabilities and expects XML back.
func (s *Server) checkMapServer(ctx context.Context) (string, error) {
	url := s.cfg.MapServerURL + "?SERVICE=WMS&REQUEST=GetCapabilities&VERSION=1.3.0"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close()
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if strings.Contains(string(head), "ServiceExceptionReport") {
		return "", fmt.Errorf("mapfile error: %s", firstLine(string(head)))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "xml") {
		return "", fmt.Errorf("unexpected content type %q", resp.Header.Get("Content-Type"))
	}
	return "WMS GetCapabilities OK", nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
