// Package store owns the embedded SQLite config database (msadmin.db).
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	DB *sql.DB
}

// Open creates the DB file if needed, sets the pragmas from PLAN.md §4 on
// every connection and applies pending migrations.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(dbPath) +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL allows concurrent readers; writes are serialized by SQLite itself.
	db.SetMaxOpenConns(4)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// Ping runs a trivial query, used by /health.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.DB.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// SchemaVersion returns PRAGMA user_version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.DB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// migrate applies migrations/NNNN_*.sql whose number is above user_version,
// each in its own transaction together with the version bump.
func (s *Store) migrate(ctx context.Context) error {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, name := range names {
		base := path.Base(name)
		num, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", base)
		}
		if num <= current {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(ctx, string(body), num); err != nil {
			return fmt.Errorf("migration %s: %w", base, err)
		}
		current = num
	}
	return nil
}

// applyMigration runs one migration with foreign keys off, so tables can be
// rebuilt (https://sqlite.org/lang_altertable.html#otheralter), and checks
// the keys before committing.
func (s *Store) applyMigration(ctx context.Context, body string, num int) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// The pragma is a no-op inside a transaction, so set it before.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	broken := rows.Next()
	rows.Close()
	if broken {
		return fmt.Errorf("foreign key check failed")
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", num)); err != nil {
		return fmt.Errorf("set version: %w", err)
	}
	return tx.Commit()
}
