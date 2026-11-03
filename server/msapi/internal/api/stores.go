package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"msapi/internal/localfs"
	"msapi/internal/mapfile"
	"msapi/internal/model"
	"msapi/internal/pgread"
	"msapi/internal/s3"
	"msapi/internal/store"
)

const connTestTimeout = 15 * time.Second

func (s *Server) routeStores(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/stores", s.createStore)
	mux.HandleFunc("POST /api/v1/stores/test", s.testUnsavedStore)
	mux.HandleFunc("GET /api/v1/stores", s.listStores)
	mux.HandleFunc("GET /api/v1/stores/{name}", s.getStore)
	mux.HandleFunc("PUT /api/v1/stores/{name}", s.updateStore)
	mux.HandleFunc("DELETE /api/v1/stores/{name}", s.deleteStore)
	mux.HandleFunc("PUT /api/v1/stores/{name}/workspace", s.moveStore)
	mux.HandleFunc("POST /api/v1/stores/{name}/test", s.testStore)
	mux.HandleFunc("GET /api/v1/stores/{name}/resources", s.storeResources)
}

// --- request / response shapes ---

type storeRequest struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Workspace  string          `json:"workspace"` // create only; "" = default. Move with PUT /stores/{name}/workspace.
	Connection json.RawMessage `json:"connection"`
}

// Secrets are pointers so PUT can tell "not sent" (keep) from "" (clear).
type postgisInput struct {
	model.PostGISConfig
	Password *string `json:"password"`
}

type s3Input struct {
	model.S3Config
	SecretKey *string `json:"secret_key"`
}

type storeResponse struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Workspace  string `json:"workspace"`
	Connection any    `json:"connection"` // never contains secrets
	HasSecret  bool   `json:"has_secret"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func toStoreResponse(row store.StoreRow) storeResponse {
	return storeResponse{
		Name:       row.Name,
		Type:       row.Type,
		Workspace:  row.Workspace,
		Connection: row.Config,
		HasSecret:  len(row.Secret) > 0,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

// storeConn is a decoded store: exactly one of PG / S3 / Local is set.
type storeConn struct {
	PG    *model.PostGISConn
	S3    *model.S3Conn
	Local *model.LocalConn
}

// parseConnection validates the connection JSON for the given type. existing
// is the stored secret to keep when the request omits it (PUT).
func (s *Server) parseConnection(typ string, raw json.RawMessage, existingSecret string) (storeConn, []byte, []string) {
	if len(raw) == 0 {
		return storeConn{}, nil, []string{"connection is required"}
	}
	switch typ {
	case model.TypePostGIS:
		var in postgisInput
		if err := strictUnmarshal(raw, &in); err != nil {
			return storeConn{}, nil, []string{"connection: " + err.Error()}
		}
		if errs := in.Normalize(); len(errs) > 0 {
			return storeConn{}, nil, errs
		}
		c := model.PostGISConn{PostGISConfig: in.PostGISConfig, Password: existingSecret}
		if in.Password != nil {
			c.Password = *in.Password
		}
		cfg, _ := json.Marshal(in.PostGISConfig)
		return storeConn{PG: &c}, cfg, nil

	case model.TypeS3:
		var in s3Input
		if err := strictUnmarshal(raw, &in); err != nil {
			return storeConn{}, nil, []string{"connection: " + err.Error()}
		}
		if errs := in.Normalize(); len(errs) > 0 {
			return storeConn{}, nil, errs
		}
		c := model.S3Conn{S3Config: in.S3Config, SecretKey: existingSecret}
		if in.SecretKey != nil {
			c.SecretKey = *in.SecretKey
		}
		if c.SecretKey == "" {
			return storeConn{}, nil, []string{"connection.secret_key is required"}
		}
		cfg, _ := json.Marshal(in.S3Config)
		return storeConn{S3: &c}, cfg, nil

	case model.TypeLocal:
		var in model.LocalConfig
		if err := strictUnmarshal(raw, &in); err != nil {
			return storeConn{}, nil, []string{"connection: " + err.Error()}
		}
		if errs := in.Normalize(); len(errs) > 0 {
			return storeConn{}, nil, errs
		}
		c := localfs.Conn(s.cfg.RasterDir, in)
		cfg, _ := json.Marshal(in)
		return storeConn{Local: &c}, cfg, nil
	}
	return storeConn{}, nil, []string{`type must be "postgis", "s3" or "local"`}
}

func (c storeConn) secret() string {
	switch {
	case c.PG != nil:
		return c.PG.Password
	case c.S3 != nil:
		return c.S3.SecretKey
	}
	return "" // local folders have no secret
}

// test runs the connection test for any store type.
func (c storeConn) test(ctx context.Context) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, connTestTimeout)
	defer cancel()
	switch {
	case c.PG != nil:
		return pgread.Test(ctx, *c.PG)
	case c.S3 != nil:
		return nil, s3.Test(ctx, *c.S3)
	}
	return nil, localfs.Test(*c.Local)
}

// loadConn decrypts a stored row into a usable connection.
func (s *Server) loadConn(row store.StoreRow) (storeConn, error) {
	secret, err := s.box.Open(row.Secret)
	if err != nil {
		return storeConn{}, err
	}
	switch row.Type {
	case model.TypePostGIS:
		var cfg model.PostGISConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return storeConn{}, err
		}
		return storeConn{PG: &model.PostGISConn{PostGISConfig: cfg, Password: secret}}, nil
	case model.TypeS3:
		var cfg model.S3Config
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return storeConn{}, err
		}
		return storeConn{S3: &model.S3Conn{S3Config: cfg, SecretKey: secret}}, nil
	case model.TypeLocal:
		var cfg model.LocalConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return storeConn{}, err
		}
		c := localfs.Conn(s.cfg.RasterDir, cfg)
		return storeConn{Local: &c}, nil
	}
	return storeConn{}, fmt.Errorf("unknown store type %q", row.Type)
}

// --- handlers ---

func (s *Server) createStore(w http.ResponseWriter, r *http.Request) {
	var req storeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := model.ValidName(req.Name); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	conn, cfg, errs := s.parseConnection(req.Type, req.Connection, "")
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid store", errs...)
		return
	}
	if _, err := s.store.GetStore(r.Context(), req.Name); err == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("store %q already exists", req.Name))
		return
	}
	if _, err := conn.test(r.Context()); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "connection test failed", err.Error())
		return
	}
	sealed, err := s.box.Seal(conn.secret())
	if err != nil {
		s.internalError(w, err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.CreateStore(r.Context(), store.StoreRow{
		Name: req.Name, Type: req.Type, Config: cfg, Secret: sealed, Workspace: req.Workspace,
	})
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("workspace %q does not exist", req.Workspace))
		return
	}
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("store %q already exists", req.Name))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "store "+row.Name+" created"); err != nil {
		if _, derr := s.store.DeleteStore(r.Context(), row.Name); derr != nil {
			s.log.Error("undo store create", "store", row.Name, "error", derr)
		}
		s.writeRebuildError(w, err)
		return
	}
	s.log.Info("store created", "store", row.Name, "type", row.Type)
	writeJSON(w, http.StatusCreated, toStoreResponse(row))
}

func (s *Server) listStores(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListStores(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	ws := r.URL.Query().Get("workspace")
	out := make([]storeResponse, 0, len(rows))
	for _, row := range rows {
		if ws == "" || row.Workspace == ws {
			out = append(out, toStoreResponse(row))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getStore(w http.ResponseWriter, r *http.Request) {
	row, ok := s.findStore(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toStoreResponse(row))
}

func (s *Server) updateStore(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.findStore(w, r)
	if !ok {
		return
	}
	var req storeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != "" && req.Name != existing.Name {
		writeError(w, http.StatusUnprocessableEntity, "a store cannot be renamed")
		return
	}
	if req.Type != "" && req.Type != existing.Type {
		writeError(w, http.StatusUnprocessableEntity, "a store's type cannot change")
		return
	}
	oldSecret, err := s.box.Open(existing.Secret)
	if err != nil {
		s.internalError(w, err)
		return
	}
	conn, cfg, errs := s.parseConnection(existing.Type, req.Connection, oldSecret)
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid store", errs...)
		return
	}
	if _, err := conn.test(r.Context()); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "connection test failed", err.Error())
		return
	}
	sealed, err := s.box.Seal(conn.secret())
	if err != nil {
		s.internalError(w, err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.UpdateStore(r.Context(), store.StoreRow{Name: existing.Name, Config: cfg, Secret: sealed})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "store not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.pools.Drop(row.Name)
	// Layers on this store may change (S3 credentials live in the mapfile).
	if err := s.rebuildLocked(r.Context(), "store "+row.Name+" updated"); err != nil {
		if _, uerr := s.store.UpdateStore(r.Context(), existing); uerr != nil {
			s.log.Error("undo store update", "store", row.Name, "error", uerr)
		}
		s.pools.Drop(row.Name)
		s.writeRebuildError(w, err)
		return
	}
	s.log.Info("store updated", "store", row.Name)
	writeJSON(w, http.StatusOK, toStoreResponse(row))
}

// moveStore: {"workspace": "<name>"}. The store's layers move with it.
func (s *Server) moveStore(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.findStore(w, r)
	if !ok {
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Workspace == existing.Workspace {
		writeJSON(w, http.StatusOK, toStoreResponse(existing))
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.store.MoveStore(r.Context(), existing.Name, req.Workspace)
	if errors.Is(err, store.ErrWorkspaceNotFound) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("workspace %q does not exist", req.Workspace))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), fmt.Sprintf("store %s moved to workspace %s", existing.Name, req.Workspace)); err != nil {
		if uerr := s.store.MoveStore(r.Context(), existing.Name, existing.Workspace); uerr != nil {
			s.log.Error("undo store move", "store", existing.Name, "error", uerr)
		}
		s.writeRebuildError(w, err)
		return
	}
	row, err := s.store.GetStore(r.Context(), existing.Name)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStoreResponse(row))
}

func (s *Server) deleteStore(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	deps, err := s.store.DeleteStore(r.Context(), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "store not found")
		return
	case err != nil:
		s.internalError(w, err)
		return
	case len(deps) > 0:
		writeJSON(w, http.StatusConflict, errorBody{
			Error:   fmt.Sprintf("store %q is used by %d layer(s)", name, len(deps)),
			Details: deps,
		})
		return
	}
	s.pools.Drop(name)
	if err := s.rebuildLocked(r.Context(), "store "+name+" deleted"); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	s.log.Info("store deleted", "store", name)
	w.WriteHeader(http.StatusNoContent)
}

type testResponse struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Info      any    `json:"info,omitempty"`
}

// testUnsavedStore tests a connection before the store is saved (UI wizard).
// When editing an existing store and the body has no password/secret, the
// stored one is used, but only if the target (host, port, database, user /
// endpoint, bucket, access key) is unchanged, so a secret is never sent to
// a different server.
func (s *Server) testUnsavedStore(w http.ResponseWriter, r *http.Request) {
	var req storeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	secret := ""
	if row, err := s.store.GetStore(r.Context(), req.Name); err == nil && row.Type == req.Type {
		if probe, _, errs := s.parseConnection(req.Type, req.Connection, noSecret); len(errs) == 0 && probe.secret() == noSecret {
			stored, err := s.loadConn(row)
			if err != nil {
				s.internalError(w, err)
				return
			}
			if sameTarget(probe, stored) {
				secret = stored.secret()
			}
		}
	}
	conn, _, errs := s.parseConnection(req.Type, req.Connection, secret)
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid store", errs...)
		return
	}
	start := time.Now()
	info, err := conn.test(r.Context())
	resp := testResponse{OK: err == nil, LatencyMS: time.Since(start).Milliseconds(), Info: info}
	if err != nil {
		resp.Error, resp.Info = err.Error(), nil
	}
	writeJSON(w, http.StatusOK, resp)
}

// noSecret marks "no secret in the request body" when probing.
const noSecret = "<msapi-no-secret>"

func sameTarget(a, b storeConn) bool {
	if a.PG != nil && b.PG != nil {
		return a.PG.Host == b.PG.Host && a.PG.Port == b.PG.Port &&
			a.PG.Database == b.PG.Database && a.PG.User == b.PG.User
	}
	if a.S3 != nil && b.S3 != nil {
		return a.S3.EndpointOrAWS() == b.S3.EndpointOrAWS() && a.S3.Bucket == b.S3.Bucket &&
			a.S3.AccessKey == b.S3.AccessKey
	}
	return false
}

func (s *Server) testStore(w http.ResponseWriter, r *http.Request) {
	row, ok := s.findStore(w, r)
	if !ok {
		return
	}
	conn, err := s.loadConn(row)
	if err != nil {
		writeJSON(w, http.StatusOK, testResponse{Error: err.Error()})
		return
	}
	start := time.Now()
	info, err := conn.test(r.Context())
	resp := testResponse{OK: err == nil, LatencyMS: time.Since(start).Milliseconds(), Info: info}
	if err != nil {
		resp.Error = err.Error()
		resp.Info = nil
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) storeResources(w http.ResponseWriter, r *http.Request) {
	row, ok := s.findStore(w, r)
	if !ok {
		return
	}
	conn, err := s.loadConn(row)
	if err != nil {
		s.internalError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if conn.PG != nil {
		pool, err := s.pools.Get(row.Name, *conn.PG)
		if err != nil {
			writeError(w, http.StatusBadGateway, "cannot connect to store", err.Error())
			return
		}
		tables, err := pgread.ListTables(ctx, pool, conn.PG.Schema)
		if err != nil {
			writeError(w, http.StatusBadGateway, "cannot list tables", err.Error())
			return
		}
		if tables == nil {
			tables = []pgread.Table{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"type": row.Type, "tables": tables})
		return
	}

	if conn.Local != nil {
		objs, truncated, err := localfs.ListTIFFs(ctx, *conn.Local)
		if err != nil {
			writeError(w, http.StatusBadGateway, "cannot list files", err.Error())
			return
		}
		if objs == nil {
			objs = []localfs.Object{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"type": row.Type, "objects": objs, "truncated": truncated})
		return
	}

	objs, truncated, err := s3.ListTIFFs(ctx, *conn.S3)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot list objects", err.Error())
		return
	}
	if objs == nil {
		objs = []s3.Object{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": row.Type, "objects": objs, "truncated": truncated})
}

// --- helpers ---

func (s *Server) findStore(w http.ResponseWriter, r *http.Request) (store.StoreRow, bool) {
	row, err := s.store.GetStore(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "store not found")
		return row, false
	}
	if err != nil {
		s.internalError(w, err)
		return row, false
	}
	return row, true
}

// writePGServiceLocked regenerates pg_service.conf from all PostGIS stores.
// Caller holds s.writeMu.
func (s *Server) writePGServiceLocked(ctx context.Context) error {
	rows, err := s.store.ListStores(ctx)
	if err != nil {
		return err
	}
	services := map[string]model.PostGISConn{}
	for _, row := range rows {
		if row.Type != model.TypePostGIS {
			continue
		}
		conn, err := s.loadConn(row)
		if err != nil {
			return fmt.Errorf("store %s: %w", row.Name, err)
		}
		services[row.Name] = *conn.PG
	}
	return mapfile.WritePGService(s.cfg.MapfileDir, services, s.cfg.MapServerGID)
}
