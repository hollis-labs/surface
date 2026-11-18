// Package storage opens SQLite databases and moves their schema forward.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // the driver
)

// Open opens (creating it, mode 0600) the SQLite database at path, in WAL mode with foreign
// keys on.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		_ = f.Close()
	} else {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies the *.sql files in dir of fsys that have not run yet, in name order,
// each in its own transaction. What has run is kept per set in surface_migrations, so
// several sets (the core's, the agent's) can share one database.
func Migrate(ctx context.Context, db *sql.DB, set string, fsys fs.FS, dir string) error {
	if dir == "" {
		dir = "."
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS surface_migrations (
		migration_set TEXT NOT NULL, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (migration_set, name))`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("migrations %q: %w", set, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM surface_migrations WHERE migration_set=? AND name=?`, set, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, name)))
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s/%s: %w", set, name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO surface_migrations (migration_set, name) VALUES (?, ?)`, set, name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

//go:embed migrations/*.sql
var coreMigrations embed.FS

// MigrateCore applies the core's own schema.
func MigrateCore(ctx context.Context, db *sql.DB) error {
	return Migrate(ctx, db, "core", coreMigrations, "migrations")
}
