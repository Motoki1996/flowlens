package dbtest

import (
	"context"

	"github.com/flowlens/api/internal/database/db"
)

// FakeTxRunner is a database.TxRunner over a FakeQuerier, for unit tests:
// fn runs directly against Q with no real transaction, since the in-memory
// fake never partially fails and so needs no rollback semantics.
type FakeTxRunner struct {
	Q *FakeQuerier
}

// RunInTx implements database.TxRunner.
func (r FakeTxRunner) RunInTx(_ context.Context, fn func(db.Querier) error) error {
	return fn(r.Q)
}
