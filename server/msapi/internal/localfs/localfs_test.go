package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"msapi/internal/model"
)

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListAndResolve(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "drone", "a.tif"))
	write(t, filepath.Join(root, "drone", "sub", "B.TIFF"))
	write(t, filepath.Join(root, "drone", "notes.txt"))
	write(t, filepath.Join(root, "drone", ".hidden", "c.tif"))
	write(t, filepath.Join(root, "other.tif"))

	c := Conn(root, model.LocalConfig{Path: "drone"})
	if err := Test(c); err != nil {
		t.Fatal(err)
	}
	objs, truncated, err := ListTIFFs(context.Background(), c)
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	got := map[string]bool{}
	for _, o := range objs {
		got[o.Path] = true
	}
	if len(got) != 2 || !got["a.tif"] || !got["sub/B.TIFF"] {
		t.Errorf("listed %v", got)
	}

	p, err := Resolve(c, "sub/B.TIFF")
	if err != nil || p != filepath.Join(root, "drone", "sub", "B.TIFF") {
		t.Errorf("resolve = %q, %v", p, err)
	}
	if _, err := Resolve(c, "missing.tif"); err == nil {
		t.Error("missing file resolved")
	}
}

func TestOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "rasters")
	write(t, filepath.Join(root, "in.tif"))
	write(t, filepath.Join(base, "secret.tif"))

	c := Conn(root, model.LocalConfig{})
	if _, err := Resolve(c, "../secret.tif"); err == nil {
		t.Error("path outside the root resolved")
	}
	if err := os.Symlink(filepath.Join(base, "secret.tif"), filepath.Join(root, "link.tif")); err == nil {
		if _, err := Resolve(c, "link.tif"); err == nil {
			t.Error("symlink to outside the root resolved")
		}
	}
	if err := Test(Conn(root, model.LocalConfig{Path: "nope"})); err == nil {
		t.Error("missing folder passed the test")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"": "", "/drone/": "drone", `a\b`: "a/b", "a//b/./c": "a/b/c"} {
		c := model.LocalConfig{Path: in}
		if errs := c.Normalize(); len(errs) > 0 || c.Path != want {
			t.Errorf("%q → %q %v, want %q", in, c.Path, errs, want)
		}
	}
	for _, bad := range []string{"../etc", "a/../../b", `a"b`} {
		c := model.LocalConfig{Path: bad}
		if errs := c.Normalize(); len(errs) == 0 {
			t.Errorf("%q accepted", bad)
		}
	}
}
