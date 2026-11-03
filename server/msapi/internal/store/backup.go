package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tablesInOrder lists config tables parents-first (foreign keys).
var tablesInOrder = []string{"workspaces", "stores", "styles", "icons", "layers", "layer_styles", "mapfile_versions"}

// Snapshot writes a consistent copy of the database to path (VACUUM INTO).
func (s *Store) Snapshot(ctx context.Context, path string) error {
	os.Remove(path)
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// RestoreFrom replaces all config rows with those of another msadmin.db
// file, in one transaction on the live database (no file swap, so open
// connections stay valid). The file is migrated to the current schema
// first; a newer schema than ours is refused.
func (s *Store) RestoreFrom(ctx context.Context, path string) error {
	cur, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	// Bring the backup up to our schema (opening runs the migrations).
	bk, err := Open(ctx, path)
	if err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	v, err := bk.SchemaVersion(ctx)
	bk.Close()
	if err != nil {
		return err
	}
	if v != cur {
		return fmt.Errorf("backup schema version %d does not match this msapi (%d)", v, cur)
	}

	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// ATTACH is per connection, so everything runs on this one.
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS bk`, filepath.ToSlash(path)); err != nil {
		return fmt.Errorf("attach backup: %w", err)
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE bk`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := len(tablesInOrder) - 1; i >= 0; i-- {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+tablesInOrder[i]); err != nil {
			return fmt.Errorf("clear %s: %w", tablesInOrder[i], err)
		}
	}
	for _, t := range tablesInOrder {
		cols, err := columns(ctx, tx, t)
		if err != nil {
			return err
		}
		list := strings.Join(cols, ", ")
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO main.%s (%s) SELECT %s FROM bk.%s`, t, list, list, t)); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}
	return tx.Commit()
}

func columns(ctx context.Context, q queryer, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, `"`+c+`"`)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("table %s has no columns", table)
	}
	return out, rows.Err()
}
