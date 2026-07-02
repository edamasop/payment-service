package repository

import "context"

type TxManager interface {
	WithTransaction(ctx context.Context, f func(ctx context.Context) error) error
}
