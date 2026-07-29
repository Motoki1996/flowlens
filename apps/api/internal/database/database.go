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

// TxRunner runs fn as a single atomic unit, so a domain service can write
// more than one table in one commit — e.g. creating a task and enqueueing
// its outbox sync_jobs row together, so an accepted request is always
// eventually pushed (docs/plans/issue-sync.md, "Sync engine"). PoolTxRunner
// is the production implementation, backed by a real pgx transaction; tests
// use dbtest.FakeTxRunner, which runs fn directly against the in-memory
// fake querier (no rollback semantics needed there, since a fake write
// never partially fails).
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(Querier) error) error
}

// PoolTxRunner is the production TxRunner, backed by a real pgx
// transaction on pool.
type PoolTxRunner struct {
	pool *pgxpool.Pool
}

// NewTxRunner builds a PoolTxRunner over pool.
func NewTxRunner(pool *pgxpool.Pool) *PoolTxRunner {
	return &PoolTxRunner{pool: pool}
}

// RunInTx begins a transaction, runs fn with a Querier scoped to it, and
// commits on success. Any error from fn (or from the commit itself) leaves
// the transaction rolled back.
func (r *PoolTxRunner) RunInTx(ctx context.Context, fn func(Querier) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit tx: %w", err)
	}
	return nil
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
