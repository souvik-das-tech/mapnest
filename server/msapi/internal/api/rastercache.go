package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"msapi/internal/inspect"
	"msapi/internal/localfs"
	"msapi/internal/model"
)

// Raster cache: rasters that are not Cloud Optimized (not tiled, no
// overviews) render very slowly when zoomed out, because MapServer reads
// the whole file every time. For those, msapi makes a Cloud Optimized copy
// in cfg.RasterCache with gdal_translate, in the background, and the
// mapfile switches to it when it is ready. The original is never changed.
// The copy can always be made again, so it is not part of backups.

type rasterCache struct {
	mu   sync.Mutex
	jobs map[string]*cacheJob // layer name → running copy
	sem  chan struct{}        // one copy at a time: they are heavy on network and CPU
}

type cacheJob struct{ cancel context.CancelFunc }

func newRasterCache() *rasterCache {
	return &rasterCache{jobs: map[string]*cacheJob{}, sem: make(chan struct{}, 1)}
}

// startRasterCache (re)starts the copy for a layer; a running one is cancelled.
func (s *Server) startRasterCache(layer string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	job := &cacheJob{cancel: cancel}
	s.cache.mu.Lock()
	if old, ok := s.cache.jobs[layer]; ok {
		old.cancel()
	}
	s.cache.jobs[layer] = job
	s.cache.mu.Unlock()
	go func() {
		defer func() {
			s.cache.mu.Lock()
			if s.cache.jobs[layer] == job { // not replaced by a newer copy
				delete(s.cache.jobs, layer)
			}
			s.cache.mu.Unlock()
			cancel()
		}()
		s.runRasterCache(ctx, layer)
	}()
}

func (s *Server) cancelRasterCache(layer string) {
	s.cache.mu.Lock()
	if j, ok := s.cache.jobs[layer]; ok {
		j.cancel()
		delete(s.cache.jobs, layer)
	}
	s.cache.mu.Unlock()
}

// updateRasterSource changes a raster layer's source under writeMu. fn
// returns false to leave the layer alone. With rebuild, the mapfile is
// regenerated (needed when the file MapServer reads changes).
func (s *Server) updateRasterSource(ctx context.Context, name string, rebuild bool, fn func(*model.RasterSource) bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	l, err := s.store.GetLayer(ctx, name)
	if err != nil {
		return err
	}
	if l.Kind != model.KindRaster {
		return nil
	}
	var src model.RasterSource
	if err := json.Unmarshal(l.Source, &src); err != nil {
		return err
	}
	if !fn(&src) {
		return nil
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	old := l
	l.Source = raw
	if _, err := s.store.UpdateLayer(ctx, l); err != nil {
		return err
	}
	if rebuild {
		if err := s.rebuildLocked(ctx, "fast copy of layer "+name+" ready"); err != nil {
			if _, uerr := s.store.UpdateLayer(ctx, old); uerr != nil {
				s.log.Error("undo raster cache update", "layer", name, "error", uerr)
			}
			return err
		}
	}
	return nil
}

func (s *Server) runRasterCache(ctx context.Context, name string) {
	l, err := s.store.GetLayer(ctx, name)
	if err != nil || l.Kind != model.KindRaster {
		return
	}
	var src model.RasterSource
	if err := json.Unmarshal(l.Source, &src); err != nil || !src.NotCOG {
		return
	}
	path := src.Path
	if err := s.updateRasterSource(ctx, name, false, func(r *model.RasterSource) bool {
		if r.Path != path {
			return false
		}
		r.CacheStatus, r.CacheError = model.CacheBuilding, ""
		return true
	}); err != nil {
		s.log.Error("raster cache: mark building", "layer", name, "error", err)
		return
	}

	select {
	case s.cache.sem <- struct{}{}:
		defer func() { <-s.cache.sem }()
	case <-ctx.Done():
		return
	}

	st, err := s.store.GetStore(ctx, l.StoreName)
	var conn storeConn
	if err == nil {
		conn, err = s.loadConn(st)
	}
	var input string
	var env []string
	switch {
	case err != nil:
	case conn.S3 != nil:
		input, env = "/vsis3/"+conn.S3.Bucket+"/"+path, inspect.GDALEnv(*conn.S3)
	case conn.Local != nil:
		input, err = localfs.Resolve(*conn.Local, path)
	default:
		err = errors.New("not a raster store")
	}

	var file string
	var size int64
	if err == nil {
		start := time.Now()
		s.log.Info("raster cache: copying", "layer", name, "source", path)
		file, size, err = s.translateCOG(ctx, name, input, env, src)
		if err == nil {
			s.log.Info("raster cache: ready", "layer", name, "bytes", size, "took", time.Since(start).Round(time.Second).String())
		}
	}
	if ctx.Err() != nil {
		// Cancelled (layer deleted, refreshed or recopied): leave no file.
		if file != "" {
			os.Remove(filepath.Join(s.cfg.RasterCache, file))
		}
		return
	}

	var previous string
	if err != nil {
		s.log.Warn("raster cache: failed", "layer", name, "error", err)
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		_ = s.updateRasterSource(context.Background(), name, false, func(r *model.RasterSource) bool {
			if r.Path != path {
				return false
			}
			r.CacheStatus, r.CacheError = model.CacheFailed, msg
			return true
		})
		return
	}
	applied := false
	err = s.updateRasterSource(context.Background(), name, true, func(r *model.RasterSource) bool {
		if r.Path != path {
			return false // the layer changed meanwhile; this copy is stale
		}
		previous = r.CacheFile
		r.CacheStatus, r.CacheFile, r.CacheBytes, r.CacheError = model.CacheReady, file, size, ""
		applied = true
		return true
	})
	if err != nil || !applied {
		if err != nil {
			s.log.Error("raster cache: switch to copy", "layer", name, "error", err)
		}
		os.Remove(filepath.Join(s.cfg.RasterCache, file))
		return
	}
	if previous != "" && previous != file {
		os.Remove(filepath.Join(s.cfg.RasterCache, previous))
	}
}

// translateCOG writes a Cloud Optimized copy and returns its file name.
// 8-bit RGB imagery is JPEG-compressed (small, fast; visually lossless at
// quality 90); everything else is lossless DEFLATE so values (classes,
// heights) stay exact.
func (s *Server) translateCOG(ctx context.Context, layer, input string, env []string, src model.RasterSource) (string, int64, error) {
	if err := os.MkdirAll(s.cfg.RasterCache, 0o755); err != nil {
		return "", 0, err
	}
	file := fmt.Sprintf("%s-%d.tif", layer, time.Now().Unix())
	tmp := filepath.Join(s.cfg.RasterCache, ".building-"+file)
	defer os.Remove(tmp)

	args := []string{"-q", "-of", "COG", "-co", "BIGTIFF=IF_SAFER", "-co", "NUM_THREADS=ALL_CPUS", "-co", "OVERVIEW_RESAMPLING=AVERAGE"}
	if src.DataType == "Byte" && src.Bands == 3 && src.NoData == nil {
		args = append(args, "-co", "COMPRESS=JPEG", "-co", "QUALITY=90")
	} else {
		args = append(args, "-co", "COMPRESS=DEFLATE", "-co", "PREDICTOR=YES")
	}
	args = append(args, input, tmp)
	cmd := exec.CommandContext(ctx, "gdal_translate", args...)
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", 0, fmt.Errorf("gdal_translate: %s", msg)
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		return "", 0, err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, filepath.Join(s.cfg.RasterCache, file)); err != nil {
		return "", 0, err
	}
	return file, fi.Size(), nil
}

// ResumeRasterCache runs at startup: copies that were interrupted, failed
// before an upgrade, or whose file went missing (e.g. after a restore) are
// made again; files no layer uses are removed.
func (s *Server) ResumeRasterCache(ctx context.Context) {
	layers, err := s.store.ListLayers(ctx)
	if err != nil {
		s.log.Error("raster cache: list layers", "error", err)
		return
	}
	used := map[string]bool{}
	var todo []string
	for _, l := range layers {
		if l.Kind != model.KindRaster {
			continue
		}
		var src model.RasterSource
		if json.Unmarshal(l.Source, &src) != nil || !src.NotCOG {
			continue
		}
		_, statErr := os.Stat(filepath.Join(s.cfg.RasterCache, src.CacheFile))
		if src.CacheStatus == model.CacheReady && src.CacheFile != "" && statErr == nil {
			used[src.CacheFile] = true
		} else {
			todo = append(todo, l.Name)
		}
	}
	// Clean up first, so the new copies' temporary files are not removed.
	entries, _ := os.ReadDir(s.cfg.RasterCache)
	for _, e := range entries {
		if !e.IsDir() && !used[e.Name()] {
			os.Remove(filepath.Join(s.cfg.RasterCache, e.Name()))
		}
	}
	for _, name := range todo {
		s.startRasterCache(name)
	}
}

// removeRasterCacheFile deletes a layer's copy (after the layer is deleted
// or no longer needs one).
func (s *Server) removeRasterCacheFile(src json.RawMessage) {
	var r model.RasterSource
	if json.Unmarshal(src, &r) == nil && r.CacheFile != "" {
		os.Remove(filepath.Join(s.cfg.RasterCache, r.CacheFile))
	}
}

// retryRasterCache: POST /layers/{name}/cache makes the copy again.
func (s *Server) retryRasterCache(w http.ResponseWriter, r *http.Request) {
	l, ok := s.findLayer(w, r)
	if !ok {
		return
	}
	var src model.RasterSource
	if l.Kind != model.KindRaster || json.Unmarshal(l.Source, &src) != nil || !src.NotCOG {
		writeError(w, http.StatusUnprocessableEntity, "only raster layers that are not Cloud Optimized get a fast copy")
		return
	}
	s.startRasterCache(l.Name)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": model.CacheBuilding})
}
