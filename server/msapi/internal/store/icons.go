package store

import (
	"context"
	"database/sql"
	"errors"
)

type IconRow struct {
	ID        int64
	Name      string // as referenced from SLD, e.g. "school.png"
	FileName  string // under mapfiles/icons/
	Mime      string
	Width     int
	Height    int
	SHA256    string
	CreatedAt string
}

const iconCols = `id, name, file_name, mime, COALESCE(width,0), COALESCE(height,0), sha256, created_at`

func scanIcon(row interface{ Scan(...any) error }) (IconRow, error) {
	var i IconRow
	err := row.Scan(&i.ID, &i.Name, &i.FileName, &i.Mime, &i.Width, &i.Height, &i.SHA256, &i.CreatedAt)
	return i, err
}

func (s *Store) ListIcons(ctx context.Context) ([]IconRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+iconCols+` FROM icons ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IconRow
	for rows.Next() {
		i, err := scanIcon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetIcon(ctx context.Context, name string) (IconRow, error) {
	i, err := scanIcon(s.DB.QueryRowContext(ctx, `SELECT `+iconCols+` FROM icons WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

func (s *Store) CreateIcon(ctx context.Context, i IconRow) (IconRow, error) {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO icons (name, file_name, mime, width, height, sha256) VALUES (?, ?, ?, ?, ?, ?)`,
		i.Name, i.FileName, i.Mime, i.Width, i.Height, i.SHA256)
	if isUniqueViolation(err) {
		return i, ErrExists
	}
	if err != nil {
		return i, err
	}
	return s.GetIcon(ctx, i.Name)
}

func (s *Store) DeleteIcon(ctx context.Context, name string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM icons WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IconFileInUse reports whether another icon row still points at the file
// (icons are deduplicated by content).
func (s *Store) IconFileInUse(ctx context.Context, fileName string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM icons WHERE file_name = ?`, fileName).Scan(&n)
	return n > 0, err
}
