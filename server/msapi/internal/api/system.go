package api

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed openapi.yaml
var openapiYAML []byte

const maxRestoreBytes = 512 << 20

func (s *Server) routeSystem(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/backup", s.backup)
	mux.HandleFunc("POST /api/v1/system/restore", s.restore)
}

// handleDocs serves the hand-written OpenAPI description. It needs sign-in
// unless MSAPI_PUBLIC_DOCS=on.
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Write(openapiYAML)
}

// Swagger UI for /api/v1/docs. Its script and styles come from the jsDelivr
// CDN at a pinned version with Subresource Integrity, so a changed file on
// the CDN is refused by the browser. The page needs internet access in the
// browser; the raw spec at /docs works offline.
const (
	swaggerCDN    = "https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/"
	swaggerCSSSRI = "sha384-wxLW6kwyHktdDGr6Pv1zgm/VGJh99lfUbzSn6HNHBENZlCN7W602k9VkGdxuFvPn"
	swaggerJSSRI  = "sha384-wmyclcVGX/WhUkdkATwhaK1X1JtiNrr2EoYJ+diV3vj4v6OC5yCeSu+yW13SYJep"

	// Relative URL: works on msapi (:8081) and behind the web UI's nginx (:3000).
	swaggerInit = `window.ui = SwaggerUIBundle({ url: "docs", dom_id: "#swagger-ui", deepLinking: true, tryItOutEnabled: true });`
	swaggerBody = `body { margin: 0; background: #fff; }`
)

var swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MapNest API</title>
  <link rel="stylesheet" href="` + swaggerCDN + `swagger-ui.css" integrity="` + swaggerCSSSRI + `" crossorigin="anonymous">
  <style>` + swaggerBody + `</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="` + swaggerCDN + `swagger-ui-bundle.js" integrity="` + swaggerJSSRI + `" crossorigin="anonymous"></script>
  <script>` + swaggerInit + `</script>
</body>
</html>
`

// swaggerCSP allows only the pinned CDN files, the inline init script (by
// hash) and API calls to this origin. Styles allow 'unsafe-inline' because
// Swagger UI sets style attributes at runtime (a hash would disable that).
var swaggerCSP = "default-src 'none'; " +
	"script-src https://cdn.jsdelivr.net '" + cspHash(swaggerInit) + "'; " +
	"style-src https://cdn.jsdelivr.net 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; font-src https://cdn.jsdelivr.net data:; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

func (s *Server) handleSwagger(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", swaggerCSP)
	w.Write([]byte(swaggerHTML))
}

// backup streams a .tar.gz with msadmin.db (VACUUM INTO snapshot) and
// icons/. The mapfile is regenerated from the database, so it is not
// included. MSAPI_SECRET_KEY is needed to use the store passwords inside;
// keep it with the backup, not in it (PLAN.md §4, §8).
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	tmpDir, err := os.MkdirTemp(s.cfg.DataDir, ".backup-*")
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer os.RemoveAll(tmpDir)
	snap := filepath.Join(tmpDir, "msadmin.db")
	if err := s.store.Snapshot(r.Context(), snap); err != nil {
		s.internalError(w, err)
		return
	}

	name := "msapi-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err = addFile(tw, snap, "msadmin.db")
	if err == nil {
		entries, _ := os.ReadDir(s.iconsDir())
		for _, e := range entries {
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".png") {
				if err = addFile(tw, filepath.Join(s.iconsDir(), e.Name()), "icons/"+e.Name()); err != nil {
					break
				}
			}
		}
	}
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err != nil {
		// Headers are sent; the client sees a truncated archive.
		s.log.Error("backup failed", "error", err)
	}
}

func addFile(tw *tar.Writer, src, name string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// restore takes the archive from /system/backup (raw body or multipart
// "file"), replaces all configuration and icons, and rebuilds the mapfile.
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var body io.Reader = http.MaxBytesReader(w, r.Body, maxRestoreBytes)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart body")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing file field")
			return
		}
		defer f.Close()
		body = f
	}

	tmpDir, err := os.MkdirTemp(s.cfg.DataDir, ".restore-*")
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer os.RemoveAll(tmpDir)
	if err := extractBackup(body, tmpDir); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "not a valid msapi backup", err.Error())
		return
	}
	dbPath := filepath.Join(tmpDir, "msadmin.db")
	if _, err := os.Stat(dbPath); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "backup has no msadmin.db")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Icons first (additive: files are content-addressed), then the rows.
	if err := os.MkdirAll(s.iconsDir(), 0o755); err != nil {
		s.internalError(w, err)
		return
	}
	icons, _ := os.ReadDir(filepath.Join(tmpDir, "icons"))
	for _, e := range icons {
		// Same rules as an upload: safe file name, a real PNG, size limit.
		if !iconNameRe.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmpDir, "icons", e.Name()))
		if err == nil && (len(data) > maxIconBytes || !isPNG(data)) {
			s.log.Warn("restore: skipped invalid icon", "file", e.Name())
			continue
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(s.iconsDir(), e.Name()), data, 0o644)
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
	}
	if err := s.store.RestoreFrom(r.Context(), dbPath); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "restore failed; nothing was changed", err.Error())
		return
	}
	s.pools.Close()
	// Secrets that cannot be decrypted mean a different MSAPI_SECRET_KEY.
	var keyWarning string
	if rows, err := s.store.ListStores(r.Context()); err == nil {
		for _, row := range rows {
			if _, err := s.box.Open(row.Secret); err != nil {
				keyWarning = "store secrets cannot be decrypted: was the backup made with another MSAPI_SECRET_KEY?"
				break
			}
		}
	}
	if err := s.rebuildLocked(r.Context(), "restored from backup"); err != nil {
		details := []string{err.Error()}
		if keyWarning != "" {
			details = append(details, keyWarning)
		}
		writeError(w, http.StatusUnprocessableEntity,
			"configuration restored, but the mapfile could not be rebuilt; the previous mapfile stays live", details...)
		return
	}
	resp := map[string]any{"ok": true}
	if keyWarning != "" {
		resp["warning"] = keyWarning
	}
	writeJSON(w, http.StatusOK, resp)
}

// extractBackup unpacks only msadmin.db and icons/*.png, rejecting paths
// that escape the directory.
func extractBackup(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(h.Name)
		switch {
		case name == "msadmin.db":
			found = true
		case strings.HasPrefix(name, "icons/") && strings.HasSuffix(name, ".png") && !strings.Contains(name[6:], "/"):
		default:
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, maxRestoreBytes)); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("msadmin.db missing")
	}
	return nil
}
