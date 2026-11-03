package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspaces(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "msadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Stores created without a workspace land in "default".
	if _, err := s.CreateStore(ctx, StoreRow{Name: "db", Type: "postgis", Config: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	st, _ := s.GetStore(ctx, "db")
	if st.Workspace != DefaultWorkspace {
		t.Errorf("workspace = %q, want default", st.Workspace)
	}
	if _, err := s.CreateStore(ctx, StoreRow{Name: "x", Type: "s3", Config: []byte(`{}`), Workspace: "nope"}); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("unknown workspace: err = %v", err)
	}

	if _, err := s.CreateWorkspace(ctx, WorkspaceRow{Name: "city", Title: "City"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkspace(ctx, WorkspaceRow{Name: "city"}); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: err = %v", err)
	}
	if _, err := s.CreateLayer(ctx, LayerRow{Name: "roads", StoreID: st.ID, Kind: "vector", Source: []byte(`{}`), Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// Moving the store moves its layers.
	if err := s.MoveStore(ctx, "db", "city"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.GetLayer(ctx, "roads")
	if l.Workspace != "city" {
		t.Errorf("layer workspace = %q, want city", l.Workspace)
	}
	w, _ := s.GetWorkspace(ctx, "city")
	if w.Stores != 1 || w.Layers != 1 {
		t.Errorf("counts = %d stores, %d layers; want 1, 1", w.Stores, w.Layers)
	}

	// A workspace with stores is not deleted; default never is.
	if deps, err := s.DeleteWorkspace(ctx, "city"); err != nil || !reflect.DeepEqual(deps, []string{"db"}) {
		t.Errorf("delete non-empty: %v, %v", deps, err)
	}
	if _, err := s.DeleteWorkspace(ctx, DefaultWorkspace); !errors.Is(err, ErrDefaultWorkspace) {
		t.Errorf("delete default: err = %v", err)
	}
	if err := s.MoveStore(ctx, "db", DefaultWorkspace); err != nil {
		t.Fatal(err)
	}
	if deps, err := s.DeleteWorkspace(ctx, "city"); err != nil || len(deps) != 0 {
		t.Errorf("delete empty: %v, %v", deps, err)
	}
	ws, _ := s.ListWorkspaces(ctx)
	if len(ws) != 1 || ws[0].Name != DefaultWorkspace {
		t.Errorf("workspaces = %+v", ws)
	}
}
