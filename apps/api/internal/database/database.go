// Package database owns the PostgreSQL connection pool. The type-safe
// query code lives in the generated sub-package ./db.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is the set of generated queries the application runs. It is
// re-exported here so wiring code can obtain one without importing the
// generated package directly.
type Querier = db.Querier

// NewQuerier binds the generated queries to a connection pool.
func NewQuerier(pool *pgxpool.Pool) Querier {
	return db.New(pool)
}

// Connect opens a pgx connection pool and verifies connectivity.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}
