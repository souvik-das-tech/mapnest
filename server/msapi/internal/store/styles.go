package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type StyleRow struct {
	ID        int64
	Name      string
	Format    string // sld | json
	Body      string
	Compiled  string
	Target    string // vector | raster
	Warnings  []string
	CreatedAt string
	UpdatedAt string
}

const styleCols = `id, name, format, body, compiled, target, COALESCE(warnings,'[]'), created_at, updated_at`

func scanStyle(row interface{ Scan(...any) error }) (StyleRow, error) {
	var s StyleRow
	var warn string
	err := row.Scan(&s.ID, &s.Name, &s.Format, &s.Body, &s.Compiled, &s.Target, &warn, &s.CreatedAt, &s.UpdatedAt)
	if err == nil {
		_ = json.Unmarshal([]byte(warn), &s.Warnings)
	}
	return s, err
}

func (s *Store) ListStyles(ctx context.Context) ([]StyleRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+styleCols+` FROM styles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StyleRow
	for rows.Next() {
		st, err := scanStyle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) GetStyle(ctx context.Context, name string) (StyleRow, error) {
	st, err := scanStyle(s.DB.QueryRowContext(ctx, `SELECT `+styleCols+` FROM styles WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}

func warningsJSON(w []string) string {
	if len(w) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(w)
	return string(b)
}

func (s *Store) CreateStyle(ctx context.Context, st StyleRow) (StyleRow, error) {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO styles (name, format, body, compiled, target, warnings) VALUES (?, ?, ?, ?, ?, ?)`,
		st.Name, st.Format, st.Body, st.Compiled, st.Target, warningsJSON(st.Warnings))
	if isUniqueViolation(err) {
		return st, ErrExists
	}
	if err != nil {
		return st, err
	}
	return s.GetStyle(ctx, st.Name)
}

func (s *Store) UpdateStyle(ctx context.Context, st StyleRow) (StyleRow, error) {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE styles SET format = ?, body = ?, compiled = ?, target = ?, warnings = ?,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE name = ?`,
		st.Format, st.Body, st.Compiled, st.Target, warningsJSON(st.Warnings), st.Name)
	if err != nil {
		return st, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return st, ErrNotFound
	}
	return s.GetStyle(ctx, st.Name)
}

// DeleteStyle removes a style. Layers using it lose it (see detachStyle); their
// names are returned.
func (s *Store) DeleteStyle(ctx context.Context, name string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM styles WHERE name = ?`, name).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	detached, err := detachStyle(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM styles WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return detached, tx.Commit()
}

// DetachStyles removes the named styles from every layer but keeps the
// styles themselves. It returns the affected layer names.
func (s *Store) DetachStyles(ctx context.Context, names []string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM styles WHERE name = ?`, name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return nil, err
		}
		layers, err := detachStyle(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		for _, l := range layers {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	return out, tx.Commit()
}

// detachStyle removes a style from every layer. Where it was the default, the
// layer's next style by name becomes the default; a layer with none left gets
// the built-in default style.
func detachStyle(ctx context.Context, tx *sql.Tx, styleID int64) ([]string, error) {
	layers, err := stringsQuery(ctx, tx, `
		SELECT l.name FROM layer_styles ls JOIN layers l ON l.id = ls.layer_id
		WHERE ls.style_id = ? ORDER BY l.name`, styleID)
	if err != nil || len(layers) == 0 {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT layer_id FROM layer_styles WHERE style_id = ? AND is_default = 1`, styleID)
	if err != nil {
		return nil, err
	}
	var promote []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		promote = append(promote, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM layer_styles WHERE style_id = ?`, styleID); err != nil {
		return nil, err
	}
	for _, layerID := range promote {
		if _, err := tx.ExecContext(ctx, `
			UPDATE layer_styles SET is_default = 1
			WHERE layer_id = ? AND style_id = (
				SELECT ls.style_id FROM layer_styles ls JOIN styles s ON s.id = ls.style_id
				WHERE ls.layer_id = ? ORDER BY s.name LIMIT 1)`, layerID, layerID); err != nil {
			return nil, err
		}
	}
	return layers, nil
}

// LayerStyle is one assignment, in display order (default first).
type LayerStyle struct {
	Style     StyleRow
	IsDefault bool
}

// LayerStyles returns the styles assigned to a layer, default first.
func (s *Store) LayerStyles(ctx context.Context, layerID int64) ([]LayerStyle, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT s.id, s.name, s.format, s.body, s.compiled, s.target, COALESCE(s.warnings,'[]'),
		       s.created_at, s.updated_at, ls.is_default
		FROM layer_styles ls JOIN styles s ON s.id = ls.style_id
		WHERE ls.layer_id = ?
		ORDER BY ls.is_default DESC, s.name`, layerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LayerStyle
	for rows.Next() {
		var ls LayerStyle
		var warn string
		if err := rows.Scan(&ls.Style.ID, &ls.Style.Name, &ls.Style.Format, &ls.Style.Body, &ls.Style.Compiled,
			&ls.Style.Target, &warn, &ls.Style.CreatedAt, &ls.Style.UpdatedAt, &ls.IsDefault); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(warn), &ls.Style.Warnings)
		out = append(out, ls)
	}
	return out, rows.Err()
}

// AllLayerStyleNames maps layer name → assigned style names (default first), for listings.
func (s *Store) AllLayerStyleNames(ctx context.Context) (map[string][]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.name, s.name FROM layer_styles ls
		JOIN layers l ON l.id = ls.layer_id JOIN styles s ON s.id = ls.style_id
		ORDER BY l.name, ls.is_default DESC, s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var l, st string
		if err := rows.Scan(&l, &st); err != nil {
			return nil, err
		}
		out[l] = append(out[l], st)
	}
	return out, rows.Err()
}

// StyleUsage maps style name → layer names using it.
func (s *Store) StyleUsage(ctx context.Context) (map[string][]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT s.name, l.name FROM layer_styles ls
		JOIN layers l ON l.id = ls.layer_id JOIN styles s ON s.id = ls.style_id
		ORDER BY s.name, l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var st, l string
		if err := rows.Scan(&st, &l); err != nil {
			return nil, err
		}
		out[st] = append(out[st], l)
	}
	return out, rows.Err()
}

// SetLayerStyles replaces a layer's assignments. styleIDs[0] is the default.
func (s *Store) SetLayerStyles(ctx context.Context, layerID int64, styleIDs []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM layer_styles WHERE layer_id = ?`, layerID); err != nil {
		return err
	}
	for i, id := range styleIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO layer_styles (layer_id, style_id, is_default) VALUES (?, ?, ?)`, layerID, id, i == 0); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func stringsQuery(ctx context.Context, q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
