package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"msapi/internal/model"
	"msapi/internal/sld"
	"msapi/internal/store"
)

const maxStyleBytes = 2 << 20

func (s *Server) routeStyles(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/styles", s.createStyle)
	mux.HandleFunc("GET /api/v1/styles", s.listStyles)
	mux.HandleFunc("POST /api/v1/styles/validate", s.validateStyle)
	mux.HandleFunc("GET /api/v1/styles/{name}", s.getStyle)
	mux.HandleFunc("GET /api/v1/styles/{name}/compiled", s.getStyleCompiled)
	mux.HandleFunc("GET /api/v1/styles/{name}/legend", s.styleLegend)
	mux.HandleFunc("PUT /api/v1/styles/{name}", s.updateStyle)
	mux.HandleFunc("DELETE /api/v1/styles/{name}", s.deleteStyle)

	mux.HandleFunc("GET /api/v1/layers/{name}/styles", s.getLayerStyles)
	mux.HandleFunc("PUT /api/v1/layers/{name}/styles", s.putLayerStyles)
	mux.HandleFunc("POST /api/v1/layers/{name}/preview", s.previewUnsavedStyle)
}

type styleResponse struct {
	Name      string   `json:"name"`
	Format    string   `json:"format"`
	Target    string   `json:"target"`
	Warnings  []string `json:"warnings"`
	UsedBy    []string `json:"used_by"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func toStyleResponse(st store.StyleRow, usedBy []string) styleResponse {
	if st.Warnings == nil {
		st.Warnings = []string{}
	}
	if usedBy == nil {
		usedBy = []string{}
	}
	return styleResponse{Name: st.Name, Format: st.Format, Target: st.Target, Warnings: st.Warnings,
		UsedBy: usedBy, CreatedAt: st.CreatedAt, UpdatedAt: st.UpdatedAt}
}

// --- icon resolution (shared with the mapfile build) ---

var nonSymbolChars = regexp.MustCompile(`[^A-Za-z0-9_]`)

// IconSymbol is the mapfile symbol name for an icon ("school.png" → "icon_school").
func IconSymbol(iconName string) string {
	base := strings.TrimSuffix(iconName, path.Ext(iconName))
	return "icon_" + nonSymbolChars.ReplaceAllString(base, "_")
}

// iconResolver matches ExternalGraphic hrefs by file name: "school.png",
// "icons/school.png" or ".../icons/school.png?x=1" all find icon school.png.
func (s *Server) iconResolver(ctx context.Context) (sld.IconResolver, error) {
	icons, err := s.store.ListIcons(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for _, ic := range icons {
		byName[strings.ToLower(ic.Name)] = IconSymbol(ic.Name)
	}
	return func(href string) (string, bool) {
		if u, err := url.Parse(href); err == nil && u.Path != "" {
			href = u.Path
		}
		base := strings.ToLower(path.Base(strings.ReplaceAll(href, `\`, "/")))
		if sym, ok := byName[base]; ok {
			return sym, true
		}
		// Also accept a reference without extension.
		if sym, ok := byName[base+".png"]; ok {
			return sym, true
		}
		return "", false
	}, nil
}

// --- compile helpers ---

// readStyleBody reads the request body and detects the format from the
// Content-Type (SLD/XML vs JSON), falling back to the first character.
func readStyleBody(w http.ResponseWriter, r *http.Request) (format string, body []byte, ok bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxStyleBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "style too large (max 2 MB)")
		return "", nil, false
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "json"):
		format = "json"
	case strings.Contains(ct, "xml"):
		format = "sld"
	default:
		t := strings.TrimSpace(string(body))
		if strings.HasPrefix(t, "{") {
			format = "json"
		} else {
			format = "sld"
		}
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "empty style body")
		return "", nil, false
	}
	return format, body, true
}

// compileStyle parses a style and renders its generic CLASS blocks.
func (s *Server) compileStyle(ctx context.Context, name, format string, body []byte) (*sld.Parsed, string, error) {
	icons, err := s.iconResolver(ctx)
	if err != nil {
		return nil, "", err
	}
	parsed, err := sld.Parse(format, body, icons)
	if err != nil {
		return nil, "", err
	}
	lt := ""
	if parsed.Target == model.KindRaster {
		lt = "RASTER"
	}
	compiled, _ := parsed.Render(name, lt)
	return parsed, compiled, nil
}

func (s *Server) writeStyleError(w http.ResponseWriter, err error) {
	var missing *sld.MissingIconsError
	switch {
	case errors.As(err, &missing):
		writeError(w, http.StatusUnprocessableEntity, "unknown icons", missing.Names...)
	case errors.Is(err, sld.ErrRendersNothing):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, http.StatusUnprocessableEntity, "invalid style", err.Error())
	}
}

// --- style handlers ---

func (s *Server) createStyle(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if err := model.ValidName(name); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "?name= "+err.Error())
		return
	}
	format, body, ok := readStyleBody(w, r)
	if !ok {
		return
	}
	parsed, compiled, err := s.compileStyle(r.Context(), name, format, body)
	if err != nil {
		s.writeStyleError(w, err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	st, err := s.store.CreateStyle(r.Context(), store.StyleRow{
		Name: name, Format: format, Body: string(body), Compiled: compiled,
		Target: parsed.Target, Warnings: parsed.Warnings,
	})
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("style %q already exists; use PUT to replace it", name))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toStyleResponse(st, nil))
}

func (s *Server) listStyles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListStyles(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	usage, err := s.store.StyleUsage(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]styleResponse, 0, len(rows))
	for _, st := range rows {
		out = append(out, toStyleResponse(st, usage[st.Name]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) findStyle(w http.ResponseWriter, r *http.Request) (store.StyleRow, bool) {
	st, err := s.store.GetStyle(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "style not found")
		return st, false
	}
	if err != nil {
		s.internalError(w, err)
		return st, false
	}
	return st, true
}

// getStyle returns the original SLD/JSON unchanged (GeoServer round-trip).
// ?meta=1 returns the JSON description instead.
func (s *Server) getStyle(w http.ResponseWriter, r *http.Request) {
	st, ok := s.findStyle(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("meta") == "1" {
		usage, _ := s.store.StyleUsage(r.Context())
		writeJSON(w, http.StatusOK, toStyleResponse(st, usage[st.Name]))
		return
	}
	if st.Format == "json" {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "application/vnd.ogc.sld+xml")
	}
	io.WriteString(w, st.Body)
}

func (s *Server) getStyleCompiled(w http.ResponseWriter, r *http.Request) {
	st, ok := s.findStyle(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, st.Compiled)
}

// validateStyle compiles without saving. ?layer=<name> renders for that
// layer's type (warnings specific to it); ?name= sets the CLASS GROUP.
func (s *Server) validateStyle(w http.ResponseWriter, r *http.Request) {
	format, body, ok := readStyleBody(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "preview"
	}
	parsed, compiled, err := s.compileStyle(r.Context(), name, format, body)
	if err != nil {
		s.writeStyleError(w, err)
		return
	}
	warnings := parsed.Warnings
	if ln := r.URL.Query().Get("layer"); ln != "" {
		l, err := s.store.GetLayer(r.Context(), ln)
		if err != nil {
			writeError(w, http.StatusNotFound, "layer not found")
			return
		}
		var lw []string
		compiled, lw = parsed.Render(name, layerTypeOf(l))
		warnings = append(warnings, lw...)
	}
	if warnings == nil {
		warnings = []string{}
	}
	usedIcons := parsed.UsedIcons
	if usedIcons == nil {
		usedIcons = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"format":     format,
		"target":     parsed.Target,
		"warnings":   warnings,
		"compiled":   compiled,
		"used_icons": usedIcons,
	})
}

func (s *Server) updateStyle(w http.ResponseWriter, r *http.Request) {
	old, ok := s.findStyle(w, r)
	if !ok {
		return
	}
	format, body, ok := readStyleBody(w, r)
	if !ok {
		return
	}
	parsed, compiled, err := s.compileStyle(r.Context(), old.Name, format, body)
	if err != nil {
		s.writeStyleError(w, err)
		return
	}
	usage, err := s.store.StyleUsage(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	if parsed.Target != old.Target && len(usage[old.Name]) > 0 {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("style is assigned to %s layers; it cannot become a %s style", old.Target, parsed.Target),
			usage[old.Name]...)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	st, err := s.store.UpdateStyle(r.Context(), store.StyleRow{
		Name: old.Name, Format: format, Body: string(body), Compiled: compiled,
		Target: parsed.Target, Warnings: parsed.Warnings,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if len(usage[old.Name]) > 0 {
		if err := s.rebuildLocked(r.Context(), "style "+old.Name+" updated"); err != nil {
			if _, uerr := s.store.UpdateStyle(r.Context(), old); uerr != nil {
				s.log.Error("undo style update", "style", old.Name, "error", uerr)
			}
			s.writeRebuildError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toStyleResponse(st, usage[old.Name]))
}

func (s *Server) deleteStyle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	detached, err := s.store.DeleteStyle(r.Context(), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "style not found")
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	// Layers that used it now fall back to another assigned style or the
	// built-in default; unassigned styles are not in the mapfile.
	if len(detached) > 0 {
		s.log.Info("style deleted; layers detached", "style", name, "layers", detached)
		if err := s.rebuildLocked(r.Context(), "style "+name+" deleted"); err != nil {
			s.writeRebuildError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// styleLegend renders the legend through MapServer GetLegendGraphic, using
// ?layer= or the first layer the style is assigned to.
func (s *Server) styleLegend(w http.ResponseWriter, r *http.Request) {
	st, ok := s.findStyle(w, r)
	if !ok {
		return
	}
	layer := r.URL.Query().Get("layer")
	if layer == "" {
		usage, err := s.store.StyleUsage(r.Context())
		if err != nil {
			s.internalError(w, err)
			return
		}
		if len(usage[st.Name]) == 0 {
			writeError(w, http.StatusConflict, "assign the style to a layer to render its legend")
			return
		}
		layer = usage[st.Name][0]
	}
	s.proxyMapServer(w, r, url.Values{
		"SERVICE":     {"WMS"},
		"VERSION":     {"1.3.0"},
		"REQUEST":     {"GetLegendGraphic"},
		"FORMAT":      {"image/png"},
		"SLD_VERSION": {"1.1.0"},
		"LAYER":       {layer},
		"STYLE":       {st.Name},
	})
}

// --- layer ↔ style assignment ---

type layerStylesBody struct {
	Default    string   `json:"default"`
	Alternates []string `json:"alternates"`
}

func (s *Server) getLayerStyles(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	ls, err := s.store.LayerStyles(r.Context(), l.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := layerStylesBody{Alternates: []string{}}
	for _, x := range ls {
		if x.IsDefault {
			out.Default = x.Style.Name
		} else {
			out.Alternates = append(out.Alternates, x.Style.Name)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// putLayerStyles sets {"default": "a", "alternates": ["b"]}. An empty
// default clears all styles (back to the built-in default style).
func (s *Server) putLayerStyles(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	var req layerStylesBody
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Default == "" && len(req.Alternates) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "alternates need a default style")
		return
	}
	var ids []int64
	seen := map[string]bool{}
	names := append([]string{}, req.Alternates...)
	if req.Default != "" {
		names = append([]string{req.Default}, names...)
	}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		st, err := s.store.GetStyle(r.Context(), n)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("style %q not found", n))
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		if st.Target != l.Kind {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("style %q is a %s style; layer %s is %s", n, st.Target, l.Name, l.Kind))
			return
		}
		ids = append(ids, st.ID)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	old, err := s.store.LayerStyles(r.Context(), l.ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.store.SetLayerStyles(r.Context(), l.ID, ids); err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "styles of layer "+l.Name+" changed"); err != nil {
		oldIDs := make([]int64, len(old))
		for i, x := range old {
			oldIDs[i] = x.Style.ID
		}
		if uerr := s.store.SetLayerStyles(r.Context(), l.ID, oldIDs); uerr != nil {
			s.log.Error("undo layer styles", "layer", l.Name, "error", uerr)
		}
		s.writeRebuildError(w, err)
		return
	}
	if req.Alternates == nil {
		req.Alternates = []string{}
	}
	writeJSON(w, http.StatusOK, req)
}

// previewUnsavedStyle renders the layer with a style from the request body
// that is not saved (style editor live preview). Query: bbox, width,
// height, srs as for GET preview.
func (s *Server) previewUnsavedStyle(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	format, body, ok := readStyleBody(w, r)
	if !ok {
		return
	}
	icons, err := s.iconResolver(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	parsed, err := sld.Parse(format, body, icons)
	if err != nil {
		s.writeStyleError(w, err)
		return
	}
	if parsed.Target != l.Kind {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("a %s style cannot preview a %s layer", parsed.Target, l.Kind))
		return
	}
	classes, _ := parsed.Render("", layerTypeOf(l)) // no GROUP: drawn without CLASSGROUP
	params, perr := s.previewParams(l, r.URL.Query())
	if perr != "" {
		writeError(w, http.StatusBadRequest, perr)
		return
	}
	params.Set("STYLES", "")
	png, err := s.renderPreview(r.Context(), l, classes, parsed.Opacity, params)
	if err != nil {
		writeError(w, http.StatusBadGateway, "preview failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// layerTypeOf returns POINT/LINE/POLYGON/RASTER for a layer row.
func layerTypeOf(l store.LayerRow) string {
	if l.Kind == model.KindRaster {
		return "RASTER"
	}
	var src model.VectorSource
	if err := json.Unmarshal(l.Source, &src); err != nil {
		return ""
	}
	return src.LayerType
}
