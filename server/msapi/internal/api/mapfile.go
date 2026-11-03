package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"msapi/internal/mapfile"
	"msapi/internal/model"
	"msapi/internal/sld"
	"msapi/internal/store"
)

// errMapfile marks problems with the generated mapfile itself (bad input
// that MapServer rejects), as opposed to I/O or database errors.
type errMapfile struct{ err error }

func (e errMapfile) Error() string { return e.err.Error() }
func (e errMapfile) Unwrap() error { return e.err }

func (s *Server) routeMapfile(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/mapfile", s.getMapfile)
	mux.HandleFunc("GET /api/v1/mapfile/versions", s.listVersions)
	mux.HandleFunc("GET /api/v1/mapfile/versions/{id}", s.getVersion)
	mux.HandleFunc("POST /api/v1/mapfile/rollback/{id}", s.rollback)
	mux.HandleFunc("POST /api/v1/mapfile/rebuild", s.rebuild)
}

// Rebuild regenerates every file MapServer reads from the database.
// Called at startup and by POST /mapfile/rebuild.
func (s *Server) Rebuild(ctx context.Context, reason string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.rebuildLocked(ctx, reason)
}

// rebuildLocked: DB → services.map (+ symbols, pg_service.conf), validated
// and swapped atomically, then recorded as a version. Caller holds writeMu.
func (s *Server) rebuildLocked(ctx context.Context, reason string) error {
	s.fontsOnce.Do(func() {
		ok, err := mapfile.WriteFonts(s.cfg.MapfileDir, s.cfg.FontDir)
		if err != nil || !ok {
			s.log.Warn("no fonts for labels; TextSymbolizers will not render", "font_dir", s.cfg.FontDir, "error", err)
		}
		s.fontset = ok
	})
	if err := s.writePGServiceLocked(ctx); err != nil {
		return fmt.Errorf("pg_service.conf: %w", err)
	}
	icons, err := s.symbolIcons(ctx)
	if err != nil {
		return err
	}
	if err := mapfile.WriteSymbols(s.cfg.MapfileDir, icons); err != nil {
		return fmt.Errorf("symbols.sym: %w", err)
	}
	m, err := s.buildMap(ctx, buildOpts{})
	if err != nil {
		return err
	}
	content, err := mapfile.Build(m)
	if err != nil {
		return fmt.Errorf("render mapfile: %w", err)
	}
	files := []mapfile.File{{Name: mapfile.MapfileName, Content: content}}

	// One more mapfile per workspace with only its layers, served at
	// /ows/<workspace>. Same layers and classes, so nothing is rebuilt.
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	for _, ws := range workspaces {
		wm := m
		wm.Title = ws.Title
		if wm.Title == "" {
			wm.Title = ws.Name
		}
		wm.PublicURL = s.workspaceURL(ws.Name)
		wm.Layers = nil
		for _, l := range m.Layers {
			if l.Workspace == ws.Name {
				wm.Layers = append(wm.Layers, l)
			}
		}
		c, err := mapfile.Build(wm)
		if err != nil {
			return fmt.Errorf("render mapfile for workspace %s: %w", ws.Name, err)
		}
		files = append(files, mapfile.File{Name: mapfile.WorkspaceFile(ws.Name), Content: c})
	}

	var validate mapfile.Validator
	if s.cfg.Validate {
		validate = mapfile.MapservValidator(s.cfg.ValidateConfig)
	}
	err = mapfile.ApplySet(ctx, s.cfg.MapfileDir, files, validate)
	var rejected *mapfile.RejectedError
	if errors.As(err, &rejected) {
		return errMapfile{err}
	}
	if err != nil {
		return err
	}
	return s.recordVersion(ctx, content, reason)
}

// workspaceURL is the public WMS/WFS URL of one workspace: <public>/ows/<name>
// (nginx and the MapServer container both route it to ws_<name>.map).
func (s *Server) workspaceURL(name string) string {
	base := strings.TrimRight(s.cfg.PublicURL, "/?")
	if !strings.HasSuffix(base, "/ows") {
		base += "/ows"
	}
	return base + "/" + name
}

func (s *Server) applyLocked(ctx context.Context, content string) error {
	var validate mapfile.Validator
	if s.cfg.Validate {
		validate = mapfile.MapservValidator(s.cfg.ValidateConfig)
	}
	err := mapfile.Apply(ctx, s.cfg.MapfileDir, content, validate)
	var rejected *mapfile.RejectedError
	if errors.As(err, &rejected) {
		return errMapfile{err}
	}
	return err
}

func (s *Server) recordVersion(ctx context.Context, content, reason string) error {
	sum := sha256.Sum256([]byte(content))
	if err := s.store.AddVersion(ctx, content, hex.EncodeToString(sum[:]), reason); err != nil {
		// The new mapfile is already live; a missing history row is not
		// worth failing the request for.
		s.log.Error("record mapfile version", "error", err)
	}
	s.log.Info("mapfile updated", "reason", reason)
	return nil
}

// symbolIcons lists every uploaded icon for symbols.sym.
func (s *Server) symbolIcons(ctx context.Context) ([]mapfile.Icon, error) {
	icons, err := s.store.ListIcons(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mapfile.Icon, 0, len(icons))
	for _, ic := range icons {
		out = append(out, mapfile.Icon{SymbolName: IconSymbol(ic.Name), File: "icons/" + ic.FileName})
	}
	return out, nil
}

// buildOpts narrows buildMap for previews.
type buildOpts struct {
	only    string   // render just this layer (enabled or not)
	classes *string  // override its CLASS blocks
	opacity *float64 // override its raster opacity
}

// buildMap turns the database into the mapfile model.
func (s *Server) buildMap(ctx context.Context, opts buildOpts) (mapfile.Map, error) {
	m := mapfile.Map{PublicURL: s.cfg.PublicURL, Fontset: s.fontset}

	storeRows, err := s.store.ListStores(ctx)
	if err != nil {
		return m, err
	}
	stores := map[string]store.StoreRow{}
	for _, r := range storeRows {
		stores[r.Name] = r
	}

	layers, err := s.store.ListLayers(ctx)
	if err != nil {
		return m, err
	}
	icons, err := s.iconResolver(ctx)
	if err != nil {
		return m, err
	}

	var s3Creds *model.S3Conn
	for _, l := range layers {
		if opts.only != "" {
			if l.Name != opts.only {
				continue
			}
		} else if !l.Enabled {
			continue
		}
		ml := mapfile.Layer{
			Workspace: l.Workspace,
			Name:      l.Name,
			Title:     l.Title,
			Abstract:  l.Abstract,
			Extent:    l.Extent,
			WFS:       l.WFSEnabled,
			MinScale:  l.MinScale,
			MaxScale:  l.MaxScale,
		}
		switch l.Kind {
		case model.KindVector:
			var src model.VectorSource
			if err := json.Unmarshal(l.Source, &src); err != nil {
				return m, fmt.Errorf("layer %s: %w", l.Name, err)
			}
			ml.Type = src.LayerType
			ml.Connection = "service=" + l.StoreName
			ml.Data = mapfile.VectorData(src)
			// Lines and polygons are drawn from simplified geometry (WFS keeps
			// the full one). Not for empty tables: MapServer would need the
			// extent of the subquery.
			if src.LayerType != "POINT" && l.Extent != nil {
				ml.MapData = mapfile.VectorMapData(src)
				ml.GeomColumn = src.GeomColumn
			}
			ml.Projection = fmt.Sprintf("EPSG:%d", src.SRID)
			ml.SRS = layerSRS(src.SRID)
			ml.PK = src.PK

		case model.KindRaster:
			var src model.RasterSource
			if err := json.Unmarshal(l.Source, &src); err != nil {
				return m, fmt.Errorf("layer %s: %w", l.Name, err)
			}
			conn, err := s.loadConn(stores[l.StoreName])
			if err != nil || (conn.S3 == nil && conn.Local == nil) {
				return m, fmt.Errorf("layer %s: store %s: %v", l.Name, l.StoreName, err)
			}
			ml.Type = "RASTER"
			switch {
			case src.CacheFile != "":
				// A Cloud Optimized copy of a non-COG file (raster cache, same
				// path in MapServer). Kept while a newer copy is being made.
				ml.Data = path.Join(filepath.ToSlash(s.cfg.RasterCache), src.CacheFile)
			case conn.Local != nil:
				// Same path in MapServer: the raster dir is mounted there too.
				ml.Data = path.Join(filepath.ToSlash(conn.Local.Dir), src.Path)
			default:
				if s3Creds == nil {
					s3Creds = conn.S3
				} else if !sameS3Creds(*s3Creds, *conn.S3) {
					return m, errMapfile{fmt.Errorf("layer %s: all S3 stores used by layers must share one endpoint and credentials for now (PLAN.md §5.2 [verify] per-bucket credentials)", l.Name)}
				}
				ml.Data = "/vsis3/" + conn.S3.Bucket + "/" + src.Path
			}
			if src.SRID > 0 {
				ml.Projection = fmt.Sprintf("EPSG:%d", src.SRID)
				ml.SRS = layerSRS(src.SRID)
			} else {
				ml.Projection = "AUTO"
			}
			ml.Processing = rasterProcessing(src)

		default:
			return m, fmt.Errorf("layer %s: unknown kind %q", l.Name, l.Kind)
		}

		var opacity *float64
		if opts.only == l.Name && opts.classes != nil {
			ml.Classes, opacity = *opts.classes, opts.opacity
		} else {
			ml.Classes, ml.ClassGroup, opacity, err = s.layerClasses(ctx, l, ml.Type, icons)
			if err != nil {
				return m, err
			}
		}
		if opacity != nil {
			o := int(*opacity*100 + 0.5)
			ml.Opacity = &o
		}
		if ml.Type == "RASTER" && strings.Contains(ml.Classes, "[pixel]") {
			// Classified rasters use raw values; drop the default stretch.
			ml.Processing = withoutScale(ml.Processing)
		}
		m.Layers = append(m.Layers, ml)
	}

	if s3Creds != nil {
		m.Config = s3Config(*s3Creds)
	}
	return m, nil
}

// layerClasses returns the CLASS blocks for a layer: every assigned style
// (each under its own GROUP, the default one as CLASSGROUP) or the
// built-in default style for its type. Styles are recompiled here for the
// layer's geometry type; this happens on rebuild, never per map request.
func (s *Server) layerClasses(ctx context.Context, l store.LayerRow, layerType string, icons sld.IconResolver) (classes, classGroup string, opacity *float64, err error) {
	assigned, err := s.store.LayerStyles(ctx, l.ID)
	if err != nil {
		return "", "", nil, err
	}
	if len(assigned) == 0 {
		return mapfile.DefaultClasses(layerType), "", nil, nil
	}
	var b strings.Builder
	for _, a := range assigned {
		parsed, err := sld.Parse(a.Style.Format, []byte(a.Style.Body), icons)
		if err != nil {
			return "", "", nil, errMapfile{fmt.Errorf("style %s (layer %s): %w", a.Style.Name, l.Name, err)}
		}
		c, _ := parsed.Render(a.Style.Name, layerType)
		b.WriteString(c)
		if a.IsDefault {
			classGroup = a.Style.Name
			opacity = parsed.Opacity
		}
	}
	return b.String(), classGroup, opacity, nil
}

func withoutScale(p []string) []string {
	out := p[:0:0]
	for _, x := range p {
		if !strings.HasPrefix(x, "SCALE=") {
			out = append(out, x)
		}
	}
	return out
}

func layerSRS(srid int) string {
	set := []string{fmt.Sprintf("EPSG:%d", srid)}
	for _, c := range []string{"EPSG:4326", "EPSG:3857", "EPSG:900913"} {
		if c != set[0] {
			set = append(set, c)
		}
	}
	return strings.Join(set, " ")
}

func rasterProcessing(src model.RasterSource) []string {
	var p []string
	switch {
	case src.Bands >= 4:
		p = append(p, "BANDS=1,2,3,4") // RGBA
	case src.Bands == 3:
		p = append(p, "BANDS=1,2,3")
	default:
		// Single band: stretch to its value range unless it is already 8-bit.
		if src.DataType != "Byte" {
			if src.Min != nil && src.Max != nil && *src.Max > *src.Min {
				p = append(p, fmt.Sprintf("SCALE=%s,%s",
					strconv.FormatFloat(*src.Min, 'f', -1, 64), strconv.FormatFloat(*src.Max, 'f', -1, 64)))
			} else {
				p = append(p, "SCALE=AUTO")
			}
		}
	}
	return append(p, "RESAMPLE=AVERAGE")
}

func sameS3Creds(a, b model.S3Conn) bool {
	return a.EndpointOrAWS() == b.EndpointOrAWS() && a.Region == b.Region &&
		a.AccessKey == b.AccessKey && a.SecretKey == b.SecretKey &&
		a.UseHTTPS == b.UseHTTPS && a.VirtualHosting == b.VirtualHosting
}

// s3Config is the MAP-level GDAL config for /vsis3/ (PLAN.md §5.2).
func s3Config(c model.S3Conn) []mapfile.KV {
	yesNo := func(b bool, y, n string) string {
		if b {
			return y
		}
		return n
	}
	kv := []mapfile.KV{
		{Key: "GDAL_DISABLE_READDIR_ON_OPEN", Value: "EMPTY_DIR"},
		{Key: "CPL_VSIL_CURL_ALLOWED_EXTENSIONS", Value: ".tif,.tiff"},
		{Key: "VSI_CACHE", Value: "TRUE"},
		{Key: "AWS_REGION", Value: c.Region},
		{Key: "AWS_ACCESS_KEY_ID", Value: c.AccessKey},
		{Key: "AWS_SECRET_ACCESS_KEY", Value: c.SecretKey},
		{Key: "AWS_HTTPS", Value: yesNo(c.UseHTTPS, "YES", "NO")},
		{Key: "AWS_VIRTUAL_HOSTING", Value: yesNo(c.VirtualHosting, "TRUE", "FALSE")},
	}
	if c.Endpoint != "" {
		kv = append(kv, mapfile.KV{Key: "AWS_S3_ENDPOINT", Value: c.Endpoint})
	}
	return kv
}

// maskSecrets hides secret CONFIG values before a mapfile leaves the API.
func maskSecrets(content string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, `CONFIG "AWS_SECRET_ACCESS_KEY"`) {
			lines[i] = l[:strings.Index(l, "CONFIG")] + `CONFIG "AWS_SECRET_ACCESS_KEY" "***"`
		}
	}
	return strings.Join(lines, "\n")
}

// writeRebuildError maps a rebuild error to an HTTP response.
func (s *Server) writeRebuildError(w http.ResponseWriter, err error) {
	var me errMapfile
	if errors.As(err, &me) {
		writeError(w, http.StatusUnprocessableEntity, "change rejected; the live mapfile was not modified", me.Error())
		return
	}
	s.internalError(w, err)
}

// --- handlers ---

func (s *Server) getMapfile(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(mapfile.MapfilePath(s.cfg.MapfileDir))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no mapfile generated yet")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(maskSecrets(string(b))))
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListVersions(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) versionFromPath(w http.ResponseWriter, r *http.Request) (store.MapfileVersion, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a number")
		return store.MapfileVersion{}, false
	}
	v, err := s.store.GetVersion(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "version not found")
		return v, false
	}
	if err != nil {
		s.internalError(w, err)
		return v, false
	}
	return v, true
}

func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	v, ok := s.versionFromPath(w, r)
	if !ok {
		return
	}
	v.Content = maskSecrets(v.Content)
	writeJSON(w, http.StatusOK, v)
}

// rollback puts an old mapfile live again. The database is not changed, so
// the next change or rebuild regenerates from the database (rule 1). It is
// an emergency switch, not an undo of API changes.
func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	v, ok := s.versionFromPath(w, r)
	if !ok {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.applyLocked(r.Context(), v.Content); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	s.recordVersion(r.Context(), v.Content, fmt.Sprintf("rollback to version %d", v.ID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": v.ID})
}

func (s *Server) rebuild(w http.ResponseWriter, r *http.Request) {
	if err := s.Rebuild(r.Context(), "manual rebuild"); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
