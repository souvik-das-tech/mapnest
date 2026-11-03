// Package pgread talks to users' PostGIS databases, read-only (PLAN.md §5.1).
// Only catalog/metadata SELECTs are run, and every session is opened with
// default_transaction_read_only=on as a second guard.
package pgread

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"msapi/internal/model"
)

// Pools caches one small pool per store. Pools keep no idle connections
// for long, so an unused store costs nothing on the user's server.
type Pools struct {
	mu    sync.Mutex
	pools map[string]*entry
}

type entry struct {
	conn model.PostGISConn
	pool *pgxpool.Pool
}

func NewPools() *Pools { return &Pools{pools: map[string]*entry{}} }

func poolConfig(c model.PostGISConn) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, err
	}
	cc := cfg.ConnConfig
	cc.Host = c.Host
	cc.Port = uint16(c.Port)
	cc.Database = c.Database
	cc.User = c.User
	cc.Password = c.Password
	cc.ConnectTimeout = 5 * time.Second
	cc.RuntimeParams["default_transaction_read_only"] = "on"
	cc.RuntimeParams["application_name"] = "msapi"
	cc.RuntimeParams["statement_timeout"] = "15000"
	if err := applySSLMode(cfg, c.SSLMode); err != nil {
		return nil, err
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.MaxConnLifetime = 30 * time.Minute
	return cfg, nil
}

// applySSLMode re-parses the TLS settings for the sslmode, keeping the rest.
func applySSLMode(cfg *pgxpool.Config, mode string) error {
	tmp, err := pgx.ParseConfig("sslmode=" + mode + " host=" + cfg.ConnConfig.Host)
	if err != nil {
		return err
	}
	cfg.ConnConfig.TLSConfig = tmp.TLSConfig
	cfg.ConnConfig.Fallbacks = nil
	for _, fb := range tmp.Fallbacks {
		fb.Port = cfg.ConnConfig.Port
		cfg.ConnConfig.Fallbacks = append(cfg.ConnConfig.Fallbacks, fb)
	}
	return nil
}

// Get returns the pool for a store, creating or replacing it if the
// connection settings changed.
func (p *Pools) Get(name string, c model.PostGISConn) (*pgxpool.Pool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.pools[name]; ok {
		if e.conn == c {
			return e.pool, nil
		}
		e.pool.Close()
		delete(p.pools, name)
	}
	cfg, err := poolConfig(c)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	p.pools[name] = &entry{conn: c, pool: pool}
	return pool, nil
}

// Drop closes the pool of a deleted or changed store.
func (p *Pools) Drop(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.pools[name]; ok {
		e.pool.Close()
		delete(p.pools, name)
	}
}

func (p *Pools) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, e := range p.pools {
		e.pool.Close()
		delete(p.pools, name)
	}
}

// TestResult describes a successful connection test.
type TestResult struct {
	ServerVersion  string `json:"server_version"`
	PostGISVersion string `json:"postgis_version"`
}

// Test connects with a throw-away connection (not pooled), so a store can be
// tested before it is saved. Fails if PostGIS is not installed; we never
// create it.
func Test(ctx context.Context, c model.PostGISConn) (TestResult, error) {
	var res TestResult
	cfg, err := poolConfig(c)
	if err != nil {
		return res, err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		return res, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())

	if err := conn.QueryRow(ctx, `SHOW server_version`).Scan(&res.ServerVersion); err != nil {
		return res, err
	}
	err = conn.QueryRow(ctx,
		`SELECT extversion FROM pg_extension WHERE extname = 'postgis'`).Scan(&res.PostGISVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, errors.New("the postgis extension is not installed in this database")
	}
	if err != nil {
		return res, err
	}
	var schemaOK bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)
		        AND has_schema_privilege($1, 'USAGE')`, c.Schema).Scan(&schemaOK); err != nil {
		return res, err
	}
	if !schemaOK {
		return res, fmt.Errorf("schema %q does not exist or user %q has no USAGE on it", c.Schema, c.User)
	}
	return res, nil
}

// Table is a publishable geometry column.
type Table struct {
	Schema        string `json:"schema"`
	Table         string `json:"table"`
	GeomColumn    string `json:"geom_column"`
	GeomType      string `json:"geom_type"`
	SRID          int    `json:"srid"`
	EstimatedRows int64  `json:"estimated_rows"`
	CanSelect     bool   `json:"can_select"`
}

// ListTables lists geometry columns in the store's schema.
func ListTables(ctx context.Context, pool *pgxpool.Pool, schema string) ([]Table, error) {
	rows, err := pool.Query(ctx, `
		SELECT g.f_table_schema, g.f_table_name, g.f_geometry_column, g.type, g.srid,
		       COALESCE(GREATEST(c.reltuples, 0), 0)::bigint,
		       has_table_privilege(c.oid, 'SELECT')
		FROM geometry_columns g
		JOIN pg_namespace n ON n.nspname = g.f_table_schema
		JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = g.f_table_name
		WHERE g.f_table_schema = $1
		ORDER BY g.f_table_name, g.f_geometry_column`, schema)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Table, error) {
		var t Table
		err := r.Scan(&t.Schema, &t.Table, &t.GeomColumn, &t.GeomType, &t.SRID, &t.EstimatedRows, &t.CanSelect)
		return t, err
	})
}
