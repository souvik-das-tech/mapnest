package store

import (
	"context"
	"database/sql"
	"errors"
)

// DefaultWorkspace always exists and cannot be deleted.
const DefaultWorkspace = "default"

var ErrDefaultWorkspace = errors.New("the default workspace cannot be deleted")

type WorkspaceRow struct {
	ID          int64
	Name        string
	Title       string
	Description string
	Stores      int
	Layers      int
	CreatedAt   string
	UpdatedAt   string
}

const workspaceSelect = `
SELECT w.id, w.name, COALESCE(w.title,''), COALESCE(w.description,''),
       (SELECT count(*) FROM stores s WHERE s.workspace_id = w.id),
       (SELECT count(*) FROM layers l JOIN stores s ON s.id = l.store_id WHERE s.workspace_id = w.id),
       w.created_at, w.updated_at
FROM workspaces w`

func scanWorkspace(row interface{ Scan(...any) error }) (WorkspaceRow, error) {
	var w WorkspaceRow
	err := row.Scan(&w.ID, &w.Name, &w.Title, &w.Description, &w.Stores, &w.Layers, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]WorkspaceRow, error) {
	rows, err := s.DB.QueryContext(ctx, workspaceSelect+` ORDER BY w.name = 'default' DESC, w.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceRow
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkspace(ctx context.Context, name string) (WorkspaceRow, error) {
	w, err := scanWorkspace(s.DB.QueryRowContext(ctx, workspaceSelect+` WHERE w.name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

func (s *Store) CreateWorkspace(ctx context.Context, w WorkspaceRow) (WorkspaceRow, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO workspaces (name, title, description) VALUES (?, ?, ?)`,
		w.Name, nullIfEmpty(w.Title), nullIfEmpty(w.Description))
	if isUniqueViolation(err) {
		return w, ErrExists
	}
	if err != nil {
		return w, err
	}
	return s.GetWorkspace(ctx, w.Name)
}

// UpdateWorkspace changes title and description. Names are fixed: they are
// part of the public service URL.
func (s *Store) UpdateWorkspace(ctx context.Context, w WorkspaceRow) (WorkspaceRow, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE workspaces SET title = ?, description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE name = ?`, nullIfEmpty(w.Title), nullIfEmpty(w.Description), w.Name)
	if err != nil {
		return w, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return w, ErrNotFound
	}
	return s.GetWorkspace(ctx, w.Name)
}

// DeleteWorkspace removes an empty workspace. If stores are in it, nothing
// is deleted and their names are returned.
func (s *Store) DeleteWorkspace(ctx context.Context, name string) ([]string, error) {
	if name == DefaultWorkspace {
		return nil, ErrDefaultWorkspace
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name = ?`, name).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	stores, err := stringsQuery(ctx, tx, `SELECT name FROM stores WHERE workspace_id = ? ORDER BY name`, id)
	if err != nil || len(stores) > 0 {
		return stores, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// MoveStore puts a store (and so its layers) into another workspace.
func (s *Store) MoveStore(ctx context.Context, store, workspace string) error {
	var wsID int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name = ?`, workspace).Scan(&wsID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceNotFound
	} else if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE stores SET workspace_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE name = ?`, wsID, store)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

var ErrWorkspaceNotFound = errors.New("workspace not found")

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
