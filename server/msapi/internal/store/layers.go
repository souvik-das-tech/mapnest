package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"msapi/internal/model"
)

// LayerRow is one layer joined with its store's name and type.
type LayerRow struct {
	ID         int64
	Name       string
	Title      string
	Abstract   string
	StoreID    int64
	StoreName  string
	StoreType  string
	Workspace  string
	Kind       string
	Source     json.RawMessage
	Extent     *model.Extent
	Enabled    bool
	WFSEnabled bool
	MinScale   *float64
	MaxScale   *float64
	CreatedAt  string
	UpdatedAt  string
}

const layerSelect = `
SELECT l.id, l.name, COALESCE(l.title,''), COALESCE(l.abstract,''), l.store_id, s.name, s.type, w.name,
       l.kind, l.source, l.minx, l.miny, l.maxx, l.maxy, l.enabled, l.wfs_enabled,
       l.min_scale, l.max_scale, l.created_at, l.updated_at
FROM layers l JOIN stores s ON s.id = l.store_id JOIN workspaces w ON w.id = s.workspace_id`

func scanLayer(row interface{ Scan(...any) error }) (LayerRow, error) {
	var l LayerRow
	var src string
	var minx, miny, maxx, maxy sql.NullFloat64
	var minScale, maxScale sql.NullFloat64
	err := row.Scan(&l.ID, &l.Name, &l.Title, &l.Abstract, &l.StoreID, &l.StoreName, &l.StoreType, &l.Workspace,
		&l.Kind, &src, &minx, &miny, &maxx, &maxy, &l.Enabled, &l.WFSEnabled,
		&minScale, &maxScale, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return l, err
	}
	l.Source = json.RawMessage(src)
	if minx.Valid && miny.Valid && maxx.Valid && maxy.Valid {
		l.Extent = &model.Extent{MinX: minx.Float64, MinY: miny.Float64, MaxX: maxx.Float64, MaxY: maxy.Float64}
	}
	if minScale.Valid {
		l.MinScale = &minScale.Float64
	}
	if maxScale.Valid {
		l.MaxScale = &maxScale.Float64
	}
	return l, nil
}

func (s *Store) ListLayers(ctx context.Context) ([]LayerRow, error) {
	rows, err := s.DB.QueryContext(ctx, layerSelect+` ORDER BY l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LayerRow
	for rows.Next() {
		l, err := scanLayer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetLayer(ctx context.Context, name string) (LayerRow, error) {
	l, err := scanLayer(s.DB.QueryRowContext(ctx, layerSelect+` WHERE l.name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

func extentArgs(e *model.Extent) []any {
	if e == nil {
		return []any{nil, nil, nil, nil}
	}
	return []any{e.MinX, e.MinY, e.MaxX, e.MaxY}
}

func (s *Store) CreateLayer(ctx context.Context, l LayerRow) (LayerRow, error) {
	args := []any{l.Name, nullStr(l.Title), nullStr(l.Abstract), l.StoreID, l.Kind, string(l.Source)}
	args = append(args, extentArgs(l.Extent)...)
	args = append(args, l.Enabled, l.WFSEnabled, l.MinScale, l.MaxScale)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO layers (name, title, abstract, store_id, kind, source, minx, miny, maxx, maxy,
		                    enabled, wfs_enabled, min_scale, max_scale)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	if isUniqueViolation(err) {
		return l, ErrExists
	}
	if err != nil {
		return l, err
	}
	return s.GetLayer(ctx, l.Name)
}

// UpdateLayer writes the editable fields plus source/extent (for refresh).
func (s *Store) UpdateLayer(ctx context.Context, l LayerRow) (LayerRow, error) {
	args := []any{nullStr(l.Title), nullStr(l.Abstract), string(l.Source)}
	args = append(args, extentArgs(l.Extent)...)
	args = append(args, l.Enabled, l.WFSEnabled, l.MinScale, l.MaxScale, l.Name)
	res, err := s.DB.ExecContext(ctx, `
		UPDATE layers SET title = ?, abstract = ?, source = ?, minx = ?, miny = ?, maxx = ?, maxy = ?,
		       enabled = ?, wfs_enabled = ?, min_scale = ?, max_scale = ?,
		       updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE name = ?`, args...)
	if err != nil {
		return l, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return l, ErrNotFound
	}
	return s.GetLayer(ctx, l.Name)
}

func (s *Store) DeleteLayer(ctx context.Context, name string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM layers WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
