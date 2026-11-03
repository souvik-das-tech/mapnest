// Package inspect reads metadata from layer sources: PostGIS tables
// (catalog queries only) and COGs (gdalinfo -json). PLAN.md §5.3, §5.4.
package inspect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"msapi/internal/model"
)

// VectorRequest names the table to inspect. GeomColumn and PK are optional
// overrides; LayerType is required only for generic GEOMETRY columns.
type VectorRequest struct {
	Schema     string
	Table      string
	GeomColumn string
	PK         string
	LayerType  string
	// SRID declares the coordinate system of a column without one (SRID 0);
	// ignored when the column has its own.
	SRID int
}

type VectorResult struct {
	Source model.VectorSource
	Extent *model.Extent // nil for an empty table
	// NoSpatialIndex: the geometry column has no GiST index, so every map
	// request reads the whole table.
	NoSpatialIndex bool
}

// InspectVector detects geometry column, type, SRID, primary key and extent.
func InspectVector(ctx context.Context, pool *pgxpool.Pool, req VectorRequest) (VectorResult, error) {
	var res VectorResult
	src := model.VectorSource{Schema: req.Schema, Table: req.Table}

	// Geometry column, type, SRID.
	q := `SELECT f_geometry_column, type, srid FROM geometry_columns
	      WHERE f_table_schema = $1 AND f_table_name = $2`
	args := []any{req.Schema, req.Table}
	if req.GeomColumn != "" {
		q += ` AND f_geometry_column = $3`
		args = append(args, req.GeomColumn)
	}
	q += ` ORDER BY f_geometry_column`
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return res, err
	}
	type gc struct {
		col, typ string
		srid     int
	}
	cols, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (gc, error) {
		var g gc
		return g, r.Scan(&g.col, &g.typ, &g.srid)
	})
	if err != nil {
		return res, err
	}
	switch {
	case len(cols) == 0 && req.GeomColumn != "":
		return res, fmt.Errorf("%s.%s has no geometry column %q", req.Schema, req.Table, req.GeomColumn)
	case len(cols) == 0:
		return res, fmt.Errorf("%s.%s has no geometry column (or is not visible to this user)", req.Schema, req.Table)
	case len(cols) > 1:
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.col
		}
		return res, fmt.Errorf("%s.%s has several geometry columns (%s); set geom_column", req.Schema, req.Table, strings.Join(names, ", "))
	}
	src.GeomColumn, src.GeomType, src.SRID = cols[0].col, strings.ToUpper(cols[0].typ), cols[0].srid
	if src.SRID <= 0 {
		if req.SRID <= 0 {
			return res, noSRIDError(ctx, pool, src)
		}
		var known bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM spatial_ref_sys WHERE srid = $1)`, req.SRID).Scan(&known); err != nil {
			return res, err
		}
		if !known {
			return res, fmt.Errorf("EPSG:%d is not a coordinate system this database knows (spatial_ref_sys); set srid to another EPSG code", req.SRID)
		}
		src.SRID, src.DeclaredSRID = req.SRID, true
	}

	src.LayerType = model.LayerTypeFor(src.GeomType)
	if req.LayerType != "" {
		src.LayerType = strings.ToUpper(req.LayerType)
	}
	switch src.LayerType {
	case "POINT", "LINE", "POLYGON":
	case "":
		return res, fmt.Errorf("geometry type %s is generic; set layer_type to POINT, LINE or POLYGON", src.GeomType)
	default:
		return res, fmt.Errorf("layer_type must be POINT, LINE or POLYGON")
	}

	// Primary key (single column only; MapServer needs one unique id column).
	src.PK = req.PK
	if src.PK == "" {
		rows, err := pool.Query(ctx, `
			SELECT a.attname
			FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = (quote_ident($1) || '.' || quote_ident($2))::regclass AND i.indisprimary`,
			req.Schema, req.Table)
		if err != nil {
			return res, err
		}
		pks, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return res, err
		}
		if len(pks) != 1 {
			return res, fmt.Errorf("%s.%s has no single-column primary key (views never do); set pk to a unique column", req.Schema, req.Table)
		}
		src.PK = pks[0]
	} else {
		var ok bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			               WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
			req.Schema, req.Table, src.PK).Scan(&ok); err != nil {
			return res, err
		}
		if !ok {
			return res, fmt.Errorf("column %q not found in %s.%s", src.PK, req.Schema, req.Table)
		}
	}

	ext, err := extent(ctx, pool, src)
	if err != nil {
		return res, err
	}
	res.Source, res.Extent = src, ext

	// Views have no indexes of their own; only check tables.
	var isTable, indexed bool
	err = pool.QueryRow(ctx, `
		SELECT c.relkind IN ('r', 'p'),
		       EXISTS (SELECT 1 FROM pg_index i
		               JOIN pg_class ic ON ic.oid = i.indexrelid
		               JOIN pg_am am ON am.oid = ic.relam
		               JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		               WHERE i.indrelid = c.oid AND am.amname IN ('gist', 'spgist', 'brin') AND a.attname = $3)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, src.Schema, src.Table, src.GeomColumn).Scan(&isTable, &indexed)
	res.NoSpatialIndex = err == nil && isTable && !indexed
	return res, nil
}

// extent uses ST_EstimatedExtent (fast, needs ANALYZE statistics) and falls
// back to ST_Extent (full scan).
func extent(ctx context.Context, pool *pgxpool.Pool, src model.VectorSource) (*model.Extent, error) {
	var minx, miny, maxx, maxy *float64
	err := pool.QueryRow(ctx, `
		SELECT ST_XMin(e), ST_YMin(e), ST_XMax(e), ST_YMax(e)
		FROM (SELECT ST_EstimatedExtent($1, $2, $3) AS e) s`,
		src.Schema, src.Table, src.GeomColumn).Scan(&minx, &miny, &maxx, &maxy)
	if err != nil || minx == nil {
		ident := pgx.Identifier{src.Schema, src.Table}.Sanitize()
		col := pgx.Identifier{src.GeomColumn}.Sanitize()
		err = pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT ST_XMin(e), ST_YMin(e), ST_XMax(e), ST_YMax(e)
			FROM (SELECT ST_Extent(%s) AS e FROM %s) s`, col, ident)).Scan(&minx, &miny, &maxx, &maxy)
		if err != nil {
			return nil, fmt.Errorf("extent: %w", err)
		}
	}
	if minx == nil || miny == nil || maxx == nil || maxy == nil {
		return nil, nil // empty table
	}
	return &model.Extent{MinX: *minx, MinY: *miny, MaxX: *maxx, MaxY: *maxy}, nil
}

// noSRIDError asks for a declared SRID and, from the data's extent, suggests
// one: coordinates inside ±180/±90 are almost always longitude/latitude.
func noSRIDError(ctx context.Context, pool *pgxpool.Pool, src model.VectorSource) error {
	msg := fmt.Sprintf("geometry column %s has no SRID (coordinate system); set srid to declare it, like GeoServer's Declared SRS", src.GeomColumn)
	if e, err := extent(ctx, pool, src); err == nil && e != nil {
		if e.MinX >= -180 && e.MaxX <= 180 && e.MinY >= -90 && e.MaxY <= 90 {
			return fmt.Errorf("%s; coordinates look like longitude/latitude, so suggested srid 4326", msg)
		}
		return fmt.Errorf("%s; coordinates are projected (e.g. metres: %.0f, %.0f), so use the EPSG code of that projection, e.g. a UTM zone", msg, e.MinX, e.MinY)
	}
	return errors.New(msg)
}

// ErrUnsafeIdentifier is returned for names that cannot be written safely
// into a mapfile DATA string.
var ErrUnsafeIdentifier = errors.New(`identifiers must not contain quotes or line breaks`)

// CheckIdentifiers rejects names that would break the generated DATA string.
func CheckIdentifiers(src model.VectorSource) error {
	for _, v := range []string{src.Schema, src.Table, src.GeomColumn, src.PK} {
		if strings.ContainsAny(v, "'\"\r\n\\") {
			return ErrUnsafeIdentifier
		}
	}
	return nil
}
