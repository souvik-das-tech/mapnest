// Package localfs lists and resolves GeoTIFFs in local raster folder stores.
// The raster directory (MSAPI_RASTER_DIR) is mounted read-only into msapi
// and MapServer at the same path, so a path that works here works there.
package localfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"msapi/internal/model"
)

// MaxObjects caps a resource listing.
const MaxObjects = 1000

// Conn resolves a stored config against the raster directory.
func Conn(root string, c model.LocalConfig) model.LocalConn {
	return model.LocalConn{LocalConfig: c, Root: root, Dir: filepath.Join(root, filepath.FromSlash(c.Path))}
}

// Test checks the folder exists, is readable and is inside the raster
// directory (also after following symlinks).
func Test(c model.LocalConn) error {
	dir, err := inside(c.Root, c.Dir)
	if err != nil {
		return err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("folder %s: %w (is the raster directory mounted?)", c.Dir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", c.Dir)
	}
	if _, err := os.ReadDir(dir); err != nil {
		return fmt.Errorf("cannot read folder %s: %w", c.Dir, err)
	}
	return nil
}

// Object is a publishable raster file.
type Object struct {
	Path         string    `json:"path"` // relative to the store folder, with /
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ListTIFFs lists .tif/.tiff files under the store folder. truncated is true
// when more than MaxObjects exist. Hidden files and folders are skipped.
func ListTIFFs(ctx context.Context, c model.LocalConn) (objs []Object, truncated bool, err error) {
	dir, err := inside(c.Root, c.Dir)
	if err != nil {
		return nil, false, err
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".tif") && !strings.HasSuffix(lower, ".tiff") {
			return nil
		}
		if len(objs) == MaxObjects {
			truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		objs = append(objs, Object{Path: filepath.ToSlash(rel), Size: info.Size(), LastModified: info.ModTime().UTC()})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return objs, truncated, nil
}

// Resolve returns the absolute file path for a raster path relative to the
// store folder, refusing anything outside the raster directory. The result
// is the unresolved path (what MapServer opens); symlinks are allowed as
// long as they point inside the raster directory.
func Resolve(c model.LocalConn, rel string) (string, error) {
	p := filepath.Join(c.Dir, filepath.FromSlash(rel))
	real, err := inside(c.Root, p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a folder", rel)
	}
	return p, nil
}

// inside follows symlinks in p and checks the result is within root.
func inside(root, p string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("raster directory %s: %w (mount it into msapi and mapserver)", root, err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s does not exist", p)
		}
		return "", err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the raster directory %s", p, root)
	}
	return real, nil
}
