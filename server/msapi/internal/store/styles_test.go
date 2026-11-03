package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeleteStyleDetachesLayers(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "msadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	st, err := s.CreateStore(ctx, StoreRow{Name: "db", Type: "postgis", Config: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	style := func(name string) StyleRow {
		r, err := s.CreateStyle(ctx, StyleRow{Name: name, Format: "sld", Target: "vector"})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	layer := func(name string, styles ...StyleRow) LayerRow {
		l, err := s.CreateLayer(ctx, LayerRow{Name: name, StoreID: st.ID, Kind: "vector", Source: []byte(`{}`), Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, x := range styles {
			ids = append(ids, x.ID)
		}
		if err := s.SetLayerStyles(ctx, l.ID, ids); err != nil {
			t.Fatal(err)
		}
		return l
	}
	defaultOf := func(l LayerRow) string {
		ls, err := s.LayerStyles(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range ls {
			if x.IsDefault {
				return x.Style.Name
			}
		}
		return ""
	}

	a, b, c := style("a"), style("b"), style("c")
	only := layer("only", a)       // falls back to the built-in default
	both := layer("both", a, c, b) // b becomes the default (next by name)
	alt := layer("alt", b, a)      // keeps b

	detached, err := s.DeleteStyle(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alt", "both", "only"}; !reflect.DeepEqual(detached, want) {
		t.Errorf("detached = %v, want %v", detached, want)
	}
	if got := defaultOf(only); got != "" {
		t.Errorf("only: default = %q, want none", got)
	}
	if got := defaultOf(both); got != "b" {
		t.Errorf("both: default = %q, want b", got)
	}
	if got := defaultOf(alt); got != "b" {
		t.Errorf("alt: default = %q, want b", got)
	}
	if _, err := s.GetStyle(ctx, "a"); err != ErrNotFound {
		t.Errorf("style a still exists: %v", err)
	}

	// DetachStyles keeps the style itself.
	if _, err := s.DetachStyles(ctx, []string{"b", "missing"}); err != nil {
		t.Fatal(err)
	}
	if got := defaultOf(both); got != "c" {
		t.Errorf("both after detaching b: default = %q, want c", got)
	}
	if _, err := s.GetStyle(ctx, "b"); err != nil {
		t.Errorf("style b should remain: %v", err)
	}
}
