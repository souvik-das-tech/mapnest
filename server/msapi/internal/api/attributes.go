package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"msapi/internal/model"
	"msapi/internal/store"
)

// Attribute columns and their values, for building classified styles
// (one colour / icon / size per value or range) in the style editor.

type attribute struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // PostgreSQL type, e.g. "integer", "text"
	Numeric bool   `json:"numeric"`
}

type valueCount struct {
	Value *string `json:"value"` // nil for NULL
	Count int64   `json:"count"`
}

type attributeStats struct {
	attribute
	Count    int64        `json:"count"`    // rows with a non-NULL value
	Distinct int64        `json:"distinct"` // distinct non-NULL values
	Values   []valueCount `json:"values"`   // most frequent first, at most ?limit
	Min      *float64     `json:"min,omitempty"`
	Max      *float64     `json:"max,omitempty"`
	// Quantiles holds ?classes+1 break values from min to max (numeric only).
	Quantiles []float64 `json:"quantiles,omitempty"`
}

func (s *Server) routeAttributes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/layers/{name}/attributes", s.listAttributes)
	mux.HandleFunc("GET /api/v1/layers/{name}/attributes/{attr}", s.attributeStats)
}

// layerTable opens the PostGIS pool and source of a vector layer.
func (s *Server) layerTable(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, model.VectorSource, bool) {
	var src model.VectorSource
	l, ok := s.findLayer(w, r)
	if !ok {
		return nil, src, false
	}
	if l.Kind != model.KindVector {
		writeError(w, http.StatusBadRequest, "attributes exist only for vector layers")
		return nil, src, false
	}
	if err := json.Unmarshal(l.Source, &src); err != nil {
		s.internalError(w, err)
		return nil, src, false
	}
	st, err := s.store.GetStore(r.Context(), l.StoreName)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "store not found")
		return nil, src, false
	} else if err != nil {
		s.internalError(w, err)
		return nil, src, false
	}
	conn, err := s.loadConn(st)
	if err != nil {
		s.internalError(w, err)
		return nil, src, false
	}
	if conn.PG == nil {
		writeError(w, http.StatusBadRequest, "layer store is not PostGIS")
		return nil, src, false
	}
	pool, err := s.pools.Get(st.Name, *conn.PG)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot connect to store", err.Error())
		return nil, src, false
	}
	return pool, src, true
}

var numericTypes = map[string]bool{
	"smallint": true, "integer": true, "bigint": true, "numeric": true, "real": true, "double precision": true,
}

func columns(ctx context.Context, pool *pgxpool.Pool, src model.VectorSource) ([]attribute, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name <> $3
		ORDER BY ordinal_position`, src.Schema, src.Table, src.GeomColumn)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (attribute, error) {
		var a attribute
		err := r.Scan(&a.Name, &a.Type)
		a.Numeric = numericTypes[a.Type]
		return a, err
	})
}

func (s *Server) listAttributes(w http.ResponseWriter, r *http.Request) {
	pool, src, ok := s.layerTable(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cols, err := columns(ctx, pool, src)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot read columns", err.Error())
		return
	}
	if cols == nil {
		cols = []attribute{}
	}
	writeJSON(w, http.StatusOK, cols)
}

// attributeStats: ?limit= (values, default 100, max 500), ?classes= (numeric
// quantile breaks, 2..20).
func (s *Server) attributeStats(w http.ResponseWriter, r *http.Request) {
	pool, src, ok := s.layerTable(w, r)
	if !ok {
		return
	}
	limit, classes := 100, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be 1 to 500")
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("classes"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 || n > 20 {
			writeError(w, http.StatusBadRequest, "classes must be 2 to 20")
			return
		}
		classes = n
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cols, err := columns(ctx, pool, src)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot read columns", err.Error())
		return
	}
	// Only a column that exists is ever written into SQL, and it is quoted.
	var out attributeStats
	found := false
	for _, c := range cols {
		if c.Name == r.PathValue("attr") {
			out.attribute, found = c, true
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "no such column")
		return
	}
	col := pgx.Identifier{out.Name}.Sanitize()
	table := pgx.Identifier{src.Schema, src.Table}.Sanitize()

	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(%[1]s), count(DISTINCT %[1]s) FROM %[2]s`, col, table)).
		Scan(&out.Count, &out.Distinct); err != nil {
		writeError(w, http.StatusBadGateway, "cannot read values", err.Error())
		return
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT %[1]s::text, count(*) FROM %[2]s GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT %[3]d`, col, table, limit))
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot read values", err.Error())
		return
	}
	out.Values, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (valueCount, error) {
		var v valueCount
		return v, r.Scan(&v.Value, &v.Count)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot read values", err.Error())
		return
	}
	if out.Values == nil {
		out.Values = []valueCount{}
	}

	if out.Numeric && out.Count > 0 {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT min(%[1]s)::float8, max(%[1]s)::float8 FROM %[2]s`, col, table)).
			Scan(&out.Min, &out.Max); err != nil {
			writeError(w, http.StatusBadGateway, "cannot read range", err.Error())
			return
		}
		if classes > 0 {
			fractions := make([]float64, classes+1)
			for i := range fractions {
				fractions[i] = float64(i) / float64(classes)
			}
			if err := pool.QueryRow(ctx, fmt.Sprintf(
				`SELECT percentile_cont($1::float8[]) WITHIN GROUP (ORDER BY %[1]s::float8) FROM %[2]s`, col, table),
				fractions).Scan(&out.Quantiles); err != nil {
				writeError(w, http.StatusBadGateway, "cannot compute quantiles", err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
