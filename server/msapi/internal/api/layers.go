package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"msapi/internal/inspect"
	"msapi/internal/localfs"
	"msapi/internal/model"
	"msapi/internal/store"
)

func (s *Server) routeLayers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/layers", s.createLayer)
	mux.HandleFunc("POST /api/v1/layers/inspect", s.inspectLayer)
	mux.HandleFunc("POST /api/v1/layers/srid-candidates", s.sridCandidates)
	mux.HandleFunc("GET /api/v1/layers", s.listLayers)
	mux.HandleFunc("GET /api/v1/layers/{name}", s.getLayer)
	mux.HandleFunc("PUT /api/v1/layers/{name}", s.updateLayer)
	mux.HandleFunc("DELETE /api/v1/layers/{name}", s.deleteLayer)
	mux.HandleFunc("POST /api/v1/layers/{name}/refresh", s.refreshLayer)
	mux.HandleFunc("POST /api/v1/layers/{name}/cache", s.retryRasterCache)
	mux.HandleFunc("GET /api/v1/layers/{name}/preview", s.previewLayer)
}

// --- request / response shapes ---

type layerRequest struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
	Store    string `json:"store"`

	// PostGIS
	Schema     string `json:"schema"`
	Table      string `json:"table"`
	GeomColumn string `json:"geom_column"`
	PK         string `json:"pk"`
	LayerType  string `json:"layer_type"`
	SRID       int    `json:"srid"` // declares the CRS of a geometry column without SRID

	// S3 COG
	Path string `json:"path"`

	Enabled    *bool    `json:"enabled"`
	WFSEnabled bool     `json:"wfs_enabled"`
	MinScale   *float64 `json:"min_scale"`
	MaxScale   *float64 `json:"max_scale"`
}

// optFloat tells "absent" from "null" in PUT bodies.
type optFloat struct {
	Set bool
	V   *float64
}

func (o *optFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.V = nil
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

type layerUpdate struct {
	Title      *string  `json:"title"`
	Abstract   *string  `json:"abstract"`
	Enabled    *bool    `json:"enabled"`
	WFSEnabled *bool    `json:"wfs_enabled"`
	MinScale   optFloat `json:"min_scale"`
	MaxScale   optFloat `json:"max_scale"`
	// SRID overrides the coordinate system (an EPSG code); 0 goes back to
	// the source's own.
	SRID *int `json:"srid"`
}

type layerResponse struct {
	Name       string          `json:"name"`
	Title      string          `json:"title"`
	Abstract   string          `json:"abstract"`
	Store      string          `json:"store"`
	Workspace  string          `json:"workspace"`
	Kind       string          `json:"kind"`
	Source     json.RawMessage `json:"source"`
	Extent     *model.Extent   `json:"extent"`
	Enabled    bool            `json:"enabled"`
	WFSEnabled bool            `json:"wfs_enabled"`
	MinScale   *float64        `json:"min_scale"`
	MaxScale   *float64        `json:"max_scale"`
	WMSURL     string          `json:"wms_url"`
	OWSURL     string          `json:"ows_url"` // the workspace's WMS/WFS endpoint
	Styles     []string        `json:"styles"`  // assigned styles, default first; empty = built-in default
	Warnings   []string        `json:"warnings,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

func (s *Server) toLayerResponse(l store.LayerRow) layerResponse {
	return layerResponse{
		Styles: []string{},
		Name:   l.Name, Title: l.Title, Abstract: l.Abstract, Store: l.StoreName, Workspace: l.Workspace, Kind: l.Kind,
		Source: l.Source, Extent: l.Extent, Enabled: l.Enabled, WFSEnabled: l.WFSEnabled,
		MinScale: l.MinScale, MaxScale: l.MaxScale, WMSURL: s.cfg.PublicURL, OWSURL: s.workspaceURL(l.Workspace),
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

// inspected is what publishing learns from the source.
type inspected struct {
	kind     string
	source   json.RawMessage
	extent   *model.Extent
	warnings []string
}

// errBadSource marks source problems the user can fix (422).
type errBadSource struct{ err error }

func (e errBadSource) Error() string { return e.err.Error() }

// inspectSource reads metadata for a new or refreshed layer.
func (s *Server) inspectSource(ctx context.Context, st store.StoreRow, req layerRequest) (inspected, error) {
	conn, err := s.loadConn(st)
	if err != nil {
		return inspected{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if conn.PG != nil {
		if req.Table == "" {
			return inspected{}, errBadSource{errors.New("table is required for a PostGIS store")}
		}
		if req.Path != "" {
			return inspected{}, errBadSource{errors.New("path is only for raster (S3 or local folder) stores")}
		}
		schema := req.Schema
		if schema == "" {
			schema = conn.PG.Schema
		}
		pool, err := s.pools.Get(st.Name, *conn.PG)
		if err != nil {
			return inspected{}, errBadSource{fmt.Errorf("cannot connect to store: %w", err)}
		}
		res, err := inspect.InspectVector(ctx, pool, inspect.VectorRequest{
			Schema: schema, Table: req.Table, GeomColumn: req.GeomColumn, PK: req.PK, LayerType: req.LayerType, SRID: req.SRID,
		})
		if err != nil {
			return inspected{}, errBadSource{err}
		}
		if err := inspect.CheckIdentifiers(res.Source); err != nil {
			return inspected{}, errBadSource{err}
		}
		src, _ := json.Marshal(res.Source)
		in := inspected{kind: model.KindVector, source: src, extent: res.Extent}
		if res.Extent == nil {
			in.warnings = append(in.warnings, "table is empty; extent unknown (use refresh after loading data)")
		}
		if res.NoSpatialIndex {
			in.warnings = append(in.warnings, fmt.Sprintf(
				`no spatial index on %s: zoomed-in maps read the whole table, which is slow for big tables. `+
					`Ask the database owner to run: CREATE INDEX ON "%s"."%s" USING GIST ("%s");`,
				res.Source.GeomColumn, res.Source.Schema, res.Source.Table, res.Source.GeomColumn))
		}
		return in, nil
	}

	if req.Path == "" {
		return inspected{}, errBadSource{errors.New("path is required for a raster store")}
	}
	if req.Table != "" {
		return inspected{}, errBadSource{errors.New("table is only for PostGIS stores")}
	}
	var input inspect.RasterInput
	if conn.Local != nil {
		if err := inspect.ValidRasterPath(req.Path); err != nil {
			return inspected{}, errBadSource{err}
		}
		file, err := localfs.Resolve(*conn.Local, req.Path)
		if err != nil {
			return inspected{}, errBadSource{err}
		}
		input = inspect.RasterInput{Path: req.Path, Data: file, Local: true}
	} else {
		input = inspect.S3Input(*conn.S3, req.Path)
	}
	input.SRID = req.SRID
	res, err := inspect.InspectRaster(ctx, input)
	if err != nil {
		return inspected{}, errBadSource{err}
	}
	src, _ := json.Marshal(res.Source)
	ext := res.Extent
	return inspected{kind: model.KindRaster, source: src, extent: &ext, warnings: res.Warnings}, nil
}

func (s *Server) writeSourceError(w http.ResponseWriter, err error) {
	var bad errBadSource
	if errors.As(err, &bad) {
		writeError(w, http.StatusUnprocessableEntity, "cannot publish", bad.Error())
		return
	}
	s.internalError(w, err)
}

// --- handlers ---

func (s *Server) createLayer(w http.ResponseWriter, r *http.Request) {
	var req layerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := model.ValidName(req.Name); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if msg := checkScales(req.MinScale, req.MaxScale); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	if _, err := s.store.GetLayer(r.Context(), req.Name); err == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("layer %q already exists", req.Name))
		return
	}
	st, err := s.store.GetStore(r.Context(), req.Store)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("store %q not found", req.Store))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	in, err := s.inspectSource(r.Context(), st, req)
	if err != nil {
		s.writeSourceError(w, err)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.CreateLayer(r.Context(), store.LayerRow{
		Name: req.Name, Title: req.Title, Abstract: req.Abstract, StoreID: st.ID,
		Kind: in.kind, Source: in.source, Extent: in.extent,
		Enabled: enabled, WFSEnabled: req.WFSEnabled, MinScale: req.MinScale, MaxScale: req.MaxScale,
	})
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("layer %q already exists", req.Name))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "layer "+row.Name+" created"); err != nil {
		if derr := s.store.DeleteLayer(r.Context(), row.Name); derr != nil {
			s.log.Error("undo layer create", "layer", row.Name, "error", derr)
		}
		s.writeRebuildError(w, err)
		return
	}
	if needsRasterCache(row.Source) {
		s.startRasterCache(row.Name) // waits for writeMu, i.e. until this request is done
	}
	resp := s.toLayerResponse(row)
	resp.Warnings = in.warnings
	writeJSON(w, http.StatusCreated, resp)
}

func needsRasterCache(src json.RawMessage) bool {
	var r model.RasterSource
	return json.Unmarshal(src, &r) == nil && r.NotCOG
}

// inspectLayer shows what publishing would detect, without saving
// (publish wizard confirmation step).
func (s *Server) inspectLayer(w http.ResponseWriter, r *http.Request) {
	var req layerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := s.store.GetStore(r.Context(), req.Store)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("store %q not found", req.Store))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	in, err := s.inspectSource(r.Context(), st, req)
	if err != nil {
		s.writeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     in.kind,
		"source":   in.source,
		"extent":   in.extent,
		"warnings": in.warnings,
	})
}

func (s *Server) listLayers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListLayers(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	styles, err := s.store.AllLayerStyleNames(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	ws := r.URL.Query().Get("workspace")
	out := make([]layerResponse, 0, len(rows))
	for _, l := range rows {
		if ws != "" && l.Workspace != ws {
			continue
		}
		resp := s.toLayerResponse(l)
		if st := styles[l.Name]; st != nil {
			resp.Styles = st
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) findLayer(w http.ResponseWriter, r *http.Request) (store.LayerRow, bool) {
	l, err := s.store.GetLayer(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "layer not found")
		return l, false
	}
	if err != nil {
		s.internalError(w, err)
		return l, false
	}
	return l, true
}

func (s *Server) getLayer(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	resp := s.toLayerResponse(l)
	if ls, err := s.store.LayerStyles(r.Context(), l.ID); err == nil {
		for _, x := range ls {
			resp.Styles = append(resp.Styles, x.Style.Name)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) updateLayer(w http.ResponseWriter, r *http.Request) {
	old, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	var req layerUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	l := old
	if req.Title != nil {
		l.Title = *req.Title
	}
	if req.Abstract != nil {
		l.Abstract = *req.Abstract
	}
	if req.Enabled != nil {
		l.Enabled = *req.Enabled
	}
	if req.WFSEnabled != nil {
		l.WFSEnabled = *req.WFSEnabled
	}
	if req.MinScale.Set {
		l.MinScale = req.MinScale.V
	}
	if req.MaxScale.Set {
		l.MaxScale = req.MaxScale.V
	}
	if msg := checkScales(l.MinScale, l.MaxScale); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	if req.SRID != nil {
		if *req.SRID < 0 || *req.SRID > 999999 {
			writeError(w, http.StatusUnprocessableEntity, "srid must be an EPSG code, or 0 for the source's own")
			return
		}
		src, err := withSRID(l.Kind, l.Source, *req.SRID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		l.Source = src
	}
	// An unknown EPSG code fails the rebuild (MapServer validates it) and
	// the old settings are restored.
	s.saveLayer(w, r, old, l, "layer "+l.Name+" updated", nil)
}

// saveLayer writes l, rebuilds, and restores old if the rebuild fails. It
// writes the response and reports whether the change was saved.
func (s *Server) saveLayer(w http.ResponseWriter, r *http.Request, old, l store.LayerRow, reason string, warnings []string) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.UpdateLayer(r.Context(), l)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "layer not found")
		return false
	}
	if err != nil {
		s.internalError(w, err)
		return false
	}
	if err := s.rebuildLocked(r.Context(), reason); err != nil {
		if _, uerr := s.store.UpdateLayer(r.Context(), old); uerr != nil {
			s.log.Error("undo layer update", "layer", l.Name, "error", uerr)
		}
		s.writeRebuildError(w, err)
		return false
	}
	resp := s.toLayerResponse(row)
	resp.Warnings = warnings
	writeJSON(w, http.StatusOK, resp)
	return true
}

func (s *Server) deleteLayer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.cancelRasterCache(name)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	old, _ := s.store.GetLayer(r.Context(), name)
	if err := s.store.DeleteLayer(r.Context(), name); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "layer not found")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "layer "+name+" deleted"); err != nil {
		// The layer is gone from the database; the next successful rebuild
		// removes it from the mapfile too.
		s.writeRebuildError(w, err)
		return
	}
	s.removeRasterCacheFile(old.Source) // the mapfile no longer uses it
	w.WriteHeader(http.StatusNoContent)
}

// refreshLayer re-reads metadata (extent, type, bands) from the source.
func (s *Server) refreshLayer(w http.ResponseWriter, r *http.Request) {
	old, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	st, err := s.store.GetStore(r.Context(), old.StoreName)
	if err != nil {
		s.internalError(w, err)
		return
	}
	req := layerRequest{Name: old.Name}
	switch old.Kind {
	case model.KindVector:
		var src model.VectorSource
		if err := json.Unmarshal(old.Source, &src); err != nil {
			s.internalError(w, err)
			return
		}
		req.Schema, req.Table, req.GeomColumn, req.PK = src.Schema, src.Table, src.GeomColumn, src.PK
		if model.LayerTypeFor(src.GeomType) == "" {
			req.LayerType = src.LayerType // keep a manual choice for generic columns
		}
		if src.DeclaredSRID {
			req.SRID = src.SRID // keep the declared CRS (used only while the column still has none)
		}
	case model.KindRaster:
		var src model.RasterSource
		if err := json.Unmarshal(old.Source, &src); err != nil {
			s.internalError(w, err)
			return
		}
		req.Path = src.Path
		if src.DeclaredSRID {
			req.SRID = src.SRID // needed again if the file has no CRS
		}
	}
	in, err := s.inspectSource(r.Context(), st, req)
	if err != nil {
		s.writeSourceError(w, err)
		return
	}
	l := old
	l.Source, l.Extent = keepRasterCache(old.Source, keepDeclaredSRID(old.Kind, old.Source, in.source)), in.extent
	if s.saveLayer(w, r, old, l, "layer "+l.Name+" refreshed", in.warnings) && old.Kind == model.KindRaster {
		if needsRasterCache(l.Source) {
			s.startRasterCache(l.Name) // the file may have changed: copy it again
		} else {
			s.removeRasterCacheFile(old.Source) // now Cloud Optimized itself
		}
	}
}

// previewLayer renders a PNG of the saved layer through MapServer.
func (s *Server) previewLayer(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	if !l.Enabled {
		writeError(w, http.StatusConflict, "layer is disabled; enable it to preview")
		return
	}
	params, msg := s.previewParams(l, r.URL.Query())
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	s.proxyMapServer(w, r, params)
}

// previewParams builds WMS 1.1.1 GetMap parameters (BBOX always x,y order)
// from ?bbox=&width=&height=&srs=&style=. Defaults: native SRS, full extent
// with 5% padding, 512 px wide. A non-empty string is a 400 message.
func (s *Server) previewParams(l store.LayerRow, q url.Values) (url.Values, string) {
	srs := q.Get("srs")
	if srs == "" {
		srs = "EPSG:4326"
		if srid := layerSRID(l); srid > 0 {
			srs = fmt.Sprintf("EPSG:%d", srid)
		}
	}

	var bbox [4]float64
	if b := q.Get("bbox"); b != "" {
		parts := strings.Split(b, ",")
		if len(parts) != 4 {
			return nil, "bbox must be minx,miny,maxx,maxy"
		}
		for i, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return nil, "bbox must be minx,miny,maxx,maxy"
			}
			bbox[i] = v
		}
	} else {
		if l.Extent == nil {
			return nil, "layer has no extent; pass bbox"
		}
		if q.Get("srs") != "" && srs != fmt.Sprintf("EPSG:%d", layerSRID(l)) {
			return nil, "bbox is required when srs differs from the layer's native SRS"
		}
		e := *l.Extent
		// Points-only layers can have zero-size extents.
		dx, dy := (e.MaxX-e.MinX)*0.05, (e.MaxY-e.MinY)*0.05
		if dx == 0 {
			dx = 0.01
		}
		if dy == 0 {
			dy = 0.01
		}
		bbox = [4]float64{e.MinX - dx, e.MinY - dy, e.MaxX + dx, e.MaxY + dy}
	}
	if bbox[2] <= bbox[0] || bbox[3] <= bbox[1] {
		return nil, "bbox is empty"
	}

	width := intParam(q.Get("width"), 512)
	height := intParam(q.Get("height"), 0)
	if height == 0 {
		height = int(float64(width) * (bbox[3] - bbox[1]) / (bbox[2] - bbox[0]))
	}
	width, height = clamp(width, 16, 2048), clamp(height, 16, 2048)

	return url.Values{
		"SERVICE":     {"WMS"},
		"VERSION":     {"1.1.1"},
		"REQUEST":     {"GetMap"},
		"LAYERS":      {l.Name},
		"STYLES":      {q.Get("style")},
		"SRS":         {srs},
		"BBOX":        {fmt.Sprintf("%g,%g,%g,%g", bbox[0], bbox[1], bbox[2], bbox[3])},
		"WIDTH":       {strconv.Itoa(width)},
		"HEIGHT":      {strconv.Itoa(height)},
		"FORMAT":      {"image/png"},
		"TRANSPARENT": {"TRUE"},
	}, ""
}

// proxyMapServer forwards a request to MapServer and streams back an image.
// MapServer errors (XML) become 502 with the message.
func (s *Server) proxyMapServer(w http.ResponseWriter, r *http.Request, params url.Values) {
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second) // MapServer IO_TIMEOUT
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.MapServerURL+"?"+params.Encode(), nil)
	if err != nil {
		s.internalError(w, err)
		return
	}
	// Previews can be slow (large rasters); don't use the 5 s health client.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "MapServer unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		writeError(w, http.StatusBadGateway, "MapServer returned an error", strings.TrimSpace(string(body)))
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, resp.Body)
}

// --- helpers ---

func layerSRID(l store.LayerRow) int {
	var src struct {
		SRID int `json:"srid"`
	}
	_ = json.Unmarshal(l.Source, &src)
	return src.SRID
}

func checkScales(minScale, maxScale *float64) string {
	if minScale != nil && *minScale < 0 || maxScale != nil && *maxScale < 0 {
		return "scales must be positive"
	}
	if minScale != nil && maxScale != nil && *minScale >= *maxScale {
		return "min_scale must be smaller than max_scale"
	}
	return ""
}

func intParam(v string, def int) int {
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// sridCandidates suggests coordinate systems for a PostGIS table whose
// geometry column has no SRID (same body as /layers/inspect), best first.
func (s *Server) sridCandidates(w http.ResponseWriter, r *http.Request) {
	var req layerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := s.store.GetStore(r.Context(), req.Store)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("store %q not found", req.Store))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	conn, err := s.loadConn(st)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if conn.PG == nil || req.Table == "" {
		writeError(w, http.StatusUnprocessableEntity, "only for PostGIS tables")
		return
	}
	pool, err := s.pools.Get(st.Name, *conn.PG)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot connect to store", err.Error())
		return
	}
	schema := req.Schema
	if schema == "" {
		schema = conn.PG.Schema
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	// ?tz_offset=<minutes east of UTC> from the browser hints at a longitude.
	var hint *float64
	if v, err := strconv.Atoi(r.URL.Query().Get("tz_offset")); err == nil && v >= -720 && v <= 840 {
		lon := float64(v) / 60 * 15
		hint = &lon
	}
	cands, ext, err := inspect.GuessSRID(ctx, pool, s.cfg.ProjDB, inspect.VectorRequest{
		Schema: schema, Table: req.Table, GeomColumn: req.GeomColumn,
	}, hint)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "cannot suggest a coordinate system", err.Error())
		return
	}
	if cands == nil {
		cands = []inspect.SRIDCandidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"extent": ext, "candidates": cands})
}

// withSRID applies a coordinate system override to a stored source.
func withSRID(kind string, raw json.RawMessage, srid int) (json.RawMessage, error) {
	switch kind {
	case model.KindVector:
		var src model.VectorSource
		if err := json.Unmarshal(raw, &src); err != nil {
			return nil, err
		}
		src, err := src.WithSRID(srid)
		if err != nil {
			return nil, err
		}
		return json.Marshal(src)
	case model.KindRaster:
		var src model.RasterSource
		if err := json.Unmarshal(raw, &src); err != nil {
			return nil, err
		}
		return json.Marshal(src.WithSRID(srid))
	}
	return nil, fmt.Errorf("unknown layer kind %q", kind)
}

// keepDeclaredSRID carries a user's SRID choice over a refresh, which
// re-reads the source and so its own SRID.
func keepDeclaredSRID(kind string, old, fresh json.RawMessage) json.RawMessage {
	var o struct {
		SRID         int  `json:"srid"`
		DeclaredSRID bool `json:"declared_srid"`
	}
	if json.Unmarshal(old, &o) != nil || !o.DeclaredSRID {
		return fresh
	}
	if out, err := withSRID(kind, fresh, o.SRID); err == nil {
		return out
	}
	return fresh
}

// keepRasterCache carries the current copy over a refresh of a non-COG
// raster (same file), so it keeps being served until the new copy is ready.
func keepRasterCache(old, fresh json.RawMessage) json.RawMessage {
	var o, f model.RasterSource
	if json.Unmarshal(old, &o) != nil || json.Unmarshal(fresh, &f) != nil || o.CacheFile == "" || o.Path != f.Path || !f.NotCOG {
		return fresh
	}
	f.CacheFile, f.CacheBytes, f.CacheStatus = o.CacheFile, o.CacheBytes, model.CacheBuilding
	if out, err := json.Marshal(f); err == nil {
		return out
	}
	return fresh
}
