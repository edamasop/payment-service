package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

type TxManager struct {
	tx *pgxpool.Pool
}

func NewTxManager(tx *pgxpool.Pool) *TxManager {
	return &TxManager{tx: tx}
}

func (t *TxManager) WithTransaction(ctx context.Context, f func(ctx context.Context) error) error {
	tx, err := t.tx.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	ctx = context.WithValue(ctx, txKey{}, tx)
	if err := f(ctx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
