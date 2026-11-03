package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return &buf
}

// A crafted backup must not write outside the extraction directory.
func TestExtractBackupPathTraversal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "extract")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarGz(t, map[string]string{
		"msadmin.db":            "db",
		"../evil.txt":           "x",
		"icons/../../evil2.png": "x",
		"icons/sub/deep.png":    "x",
		"/etc/abs.png":          "x",
		"icons/ok.png":          "png",
	})
	if err := extractBackup(archive, dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "evil.txt"), filepath.Join(root, "evil2.png"), filepath.Join(dir, "icons", "sub")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "icons", "ok.png")); err != nil {
		t.Errorf("valid icon missing: %v", err)
	}
}

func TestExtractBackupNeedsDatabase(t *testing.T) {
	if err := extractBackup(tarGz(t, map[string]string{"icons/a.png": "x"}), t.TempDir()); err == nil {
		t.Fatal("archive without msadmin.db accepted")
	}
}
