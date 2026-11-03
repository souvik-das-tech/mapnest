package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"msapi/internal/sld"
	"msapi/internal/store"
)

const maxIconBytes = 2 << 20

var iconNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\.png$`)

// isPNG reports whether data parses as a PNG within the icon size limits.
func isPNG(data []byte) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	return err == nil && cfg.Width <= 512 && cfg.Height <= 512
}

func (s *Server) routeIcons(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/icons", s.uploadIcon)
	mux.HandleFunc("GET /api/v1/icons", s.listIcons)
	mux.HandleFunc("GET /api/v1/icons/{name}", s.getIcon)
	mux.HandleFunc("DELETE /api/v1/icons/{name}", s.deleteIcon)
}

type iconResponse struct {
	Name      string   `json:"name"`
	Mime      string   `json:"mime"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	SHA256    string   `json:"sha256"`
	Symbol    string   `json:"symbol"`
	UsedBy    []string `json:"used_by"` // style names
	CreatedAt string   `json:"created_at"`
}

func toIconResponse(i store.IconRow, usedBy []string) iconResponse {
	if usedBy == nil {
		usedBy = []string{}
	}
	return iconResponse{Name: i.Name, Mime: i.Mime, Width: i.Width, Height: i.Height, SHA256: i.SHA256,
		Symbol: IconSymbol(i.Name), UsedBy: usedBy, CreatedAt: i.CreatedAt}
}

func (s *Server) iconsDir() string { return filepath.Join(s.cfg.MapfileDir, "icons") }

// iconUsage maps icon symbol → style names that reference it.
func (s *Server) iconUsage(r *http.Request) (map[string][]string, error) {
	styles, err := s.store.ListStyles(r.Context())
	if err != nil {
		return nil, err
	}
	icons, err := s.iconResolver(r.Context())
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, st := range styles {
		parsed, err := sld.Parse(st.Format, []byte(st.Body), icons)
		if err != nil {
			continue
		}
		for _, sym := range parsed.UsedIcons {
			out[sym] = append(out[sym], st.Name)
		}
	}
	return out, nil
}

// uploadIcon: multipart "file" (PNG), optional "name" (defaults to the
// file name). Files are stored by content hash, so re-uploads dedupe.
func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIconBytes+64<<10)
	if err := r.ParseMultipartForm(maxIconBytes); err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart form with a file field (max 2 MB)")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxIconBytes+1))
	if err != nil || len(data) > maxIconBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "icon too large (max 2 MB)")
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = path.Base(strings.ReplaceAll(hdr.Filename, `\`, "/"))
	}
	if !strings.HasSuffix(strings.ToLower(name), ".png") {
		name += ".png"
	}
	if !iconNameRe.MatchString(name) {
		writeError(w, http.StatusUnprocessableEntity,
			"icon name must be letters, digits, _ . - and end with .png", name)
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "only PNG icons are supported (SVG pending PLAN.md §6.4 [verify])")
		return
	}
	if cfg.Width > 512 || cfg.Height > 512 {
		writeError(w, http.StatusUnprocessableEntity, "icon must be at most 512×512 px")
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	fileName := hash[:16] + ".png"

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.store.GetIcon(r.Context(), name); err == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("icon %q already exists; delete it first to replace it", name))
		return
	}
	if err := os.MkdirAll(s.iconsDir(), 0o755); err != nil {
		s.internalError(w, err)
		return
	}
	dst := filepath.Join(s.iconsDir(), fileName)
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			s.internalError(w, err)
			return
		}
	}
	row, err := s.store.CreateIcon(r.Context(), store.IconRow{
		Name: name, FileName: fileName, Mime: "image/png", Width: cfg.Width, Height: cfg.Height, SHA256: hash,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "icon "+name+" added"); err != nil {
		s.store.DeleteIcon(r.Context(), name)
		s.writeRebuildError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toIconResponse(row, nil))
}

func (s *Server) listIcons(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListIcons(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	usage, err := s.iconUsage(r)
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]iconResponse, 0, len(rows))
	for _, i := range rows {
		out = append(out, toIconResponse(i, usage[IconSymbol(i.Name)]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getIcon(w http.ResponseWriter, r *http.Request) {
	i, err := s.store.GetIcon(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "icon not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", i.Mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", `"`+i.SHA256[:16]+`"`)
	http.ServeFile(w, r, filepath.Join(s.iconsDir(), i.FileName))
}

func (s *Server) deleteIcon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	i, err := s.store.GetIcon(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "icon not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	usage, err := s.iconUsage(r)
	if err != nil {
		s.internalError(w, err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	// Styles that draw this icon would no longer compile, so take them off
	// their layers (which fall back to the default style). The styles stay
	// in the library; re-uploading the icon makes them usable again.
	if deps := usage[IconSymbol(name)]; len(deps) > 0 {
		detached, err := s.store.DetachStyles(r.Context(), deps)
		if err != nil {
			s.internalError(w, err)
			return
		}
		s.log.Info("icon deleted; styles detached", "icon", name, "styles", deps, "layers", detached)
	}
	if err := s.store.DeleteIcon(r.Context(), name); err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.rebuildLocked(r.Context(), "icon "+name+" deleted"); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	if inUse, err := s.store.IconFileInUse(r.Context(), i.FileName); err == nil && !inUse {
		os.Remove(filepath.Join(s.iconsDir(), i.FileName))
	}
	w.WriteHeader(http.StatusNoContent)
}
