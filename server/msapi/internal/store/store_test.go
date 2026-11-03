package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "msadmin.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil || v < 1 {
		t.Fatalf("schema version = %d, %v; want >= 1", v, err)
	}

	for _, table := range []string{"stores", "layers", "styles", "layer_styles", "icons", "mapfile_versions"} {
		var n int
		if err := s.DB.QueryRowContext(ctx,
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (%v)", table, err)
		}
	}

	var mode string
	if err := s.DB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v; want wal", mode, err)
	}
	var fk int
	if err := s.DB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want 1", fk, err)
	}
	s.Close()

	// Reopening must not re-run migrations.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	v2, _ := s2.SchemaVersion(ctx)
	if v2 != v {
		t.Errorf("version after reopen = %d, want %d", v2, v)
	}
}

func TestForeignKeyRestrict(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "msadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO stores (name, type, config) VALUES ('db', 'postgis', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO layers (name, store_id, kind, source) VALUES ('l', 1, 'vector', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM stores WHERE id = 1`); err == nil {
		t.Error("deleting a store with layers should fail")
	}
}
