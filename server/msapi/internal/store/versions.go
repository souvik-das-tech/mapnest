package store

import (
	"context"
	"database/sql"
	"errors"
)

// KeepVersions is how many generated mapfiles are kept (PLAN.md §3 rule 5).
const KeepVersions = 50

type MapfileVersion struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
	SHA256    string `json:"sha256"`
	Reason    string `json:"reason"`
	Content   string `json:"content,omitempty"`
}

// AddVersion records a generated mapfile and prunes old ones. Nothing is
// recorded if the content equals the latest version.
func (s *Store) AddVersion(ctx context.Context, content, sha, reason string) error {
	var last string
	err := s.DB.QueryRowContext(ctx, `SELECT sha256 FROM mapfile_versions ORDER BY id DESC LIMIT 1`).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if last == sha {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO mapfile_versions (content, sha256, reason) VALUES (?, ?, ?)`, content, sha, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM mapfile_versions WHERE id NOT IN
		  (SELECT id FROM mapfile_versions ORDER BY id DESC LIMIT ?)`, KeepVersions); err != nil {
		return err
	}
	return tx.Commit()
}

// ListVersions returns versions newest first, without content.
func (s *Store) ListVersions(ctx context.Context) ([]MapfileVersion, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, created_at, sha256, COALESCE(reason,'') FROM mapfile_versions ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MapfileVersion{}
	for rows.Next() {
		var v MapfileVersion
		if err := rows.Scan(&v.ID, &v.CreatedAt, &v.SHA256, &v.Reason); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetVersion(ctx context.Context, id int64) (MapfileVersion, error) {
	var v MapfileVersion
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, created_at, sha256, COALESCE(reason,''), content FROM mapfile_versions WHERE id = ?`, id).
		Scan(&v.ID, &v.CreatedAt, &v.SHA256, &v.Reason, &v.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
