package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// StoreRow is one row of the stores table. Config is the JSON without secrets;
// Secret is the AES-GCM sealed password / S3 secret key.
type StoreRow struct {
	ID        int64
	Name      string
	Type      string
	Config    json.RawMessage
	Secret    []byte
	Workspace string // workspace name; CreateStore uses "default" when empty
	CreatedAt string
	UpdatedAt string
}

const storeSelect = `SELECT s.id, s.name, s.type, s.config, s.secret, w.name, s.created_at, s.updated_at
FROM stores s JOIN workspaces w ON w.id = s.workspace_id`

func scanStore(row interface{ Scan(...any) error }) (StoreRow, error) {
	var s StoreRow
	var cfg string
	err := row.Scan(&s.ID, &s.Name, &s.Type, &cfg, &s.Secret, &s.Workspace, &s.CreatedAt, &s.UpdatedAt)
	s.Config = json.RawMessage(cfg)
	return s, err
}

func (s *Store) ListStores(ctx context.Context) ([]StoreRow, error) {
	rows, err := s.DB.QueryContext(ctx, storeSelect+` ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoreRow
	for rows.Next() {
		r, err := scanStore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetStore(ctx context.Context, name string) (StoreRow, error) {
	r, err := scanStore(s.DB.QueryRowContext(ctx, storeSelect+` WHERE s.name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateStore(ctx context.Context, r StoreRow) (StoreRow, error) {
	if r.Workspace == "" {
		r.Workspace = DefaultWorkspace
	}
	var wsID int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name = ?`, r.Workspace).Scan(&wsID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrWorkspaceNotFound
	} else if err != nil {
		return r, err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO stores (name, type, config, secret, workspace_id) VALUES (?, ?, ?, ?, ?)`,
		r.Name, r.Type, string(r.Config), r.Secret, wsID)
	if isUniqueViolation(err) {
		return r, ErrExists
	}
	if err != nil {
		return r, err
	}
	return s.GetStore(ctx, r.Name)
}

// UpdateStore replaces config and secret. The type cannot change.
func (s *Store) UpdateStore(ctx context.Context, r StoreRow) (StoreRow, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE stores SET config = ?, secret = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE name = ?`,
		string(r.Config), r.Secret, r.Name)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, ErrNotFound
	}
	return s.GetStore(ctx, r.Name)
}

// DeleteStore removes a store. If layers use it, nothing is deleted and their
// names are returned.
func (s *Store) DeleteStore(ctx context.Context, name string) (dependents []string, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE name = ?`, name).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM layers WHERE store_id = ? ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, n)
	}
	rows.Close()
	if len(dependents) > 0 {
		return dependents, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stores WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
