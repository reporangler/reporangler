// Package pg holds shared Postgres helpers: connection-with-retry and a tiny
// embedded-SQL migrator. Used by auth-service and metadata-service.
package pg

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pool and waits (up to ~30s) for Postgres to accept
// connections, so services can start alongside the database.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 30; i++ {
		if err = pool.Ping(ctx); err == nil {
			return pool, nil
		}
		time.Sleep(time.Second)
	}
	pool.Close()
	return nil, fmt.Errorf("postgres not ready: %w", err)
}

// Migrate executes every *.sql file under "migrations" in the given embedded FS,
// in filename order. Migrations must be idempotent (IF NOT EXISTS / ON CONFLICT).
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := fs.ReadFile(migrations, "migrations/"+name)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}
