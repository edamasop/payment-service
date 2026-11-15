package postgres

import (
	"context"
	"payment-service/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PaymentRepository struct {
	db *pgxpool.Pool
}

func NewPaymentRepository(db *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{
		db: db,
	}
}

func (r *PaymentRepository) querier(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}

	return r.db
}

func (r *PaymentRepository) Create(ctx context.Context, p *model.Payment) error {
	q := r.querier(ctx)

	return q.QueryRow(
		ctx,
		`INSERT INTO payments 
		(order_id, customer_id, status, total_amount, currency)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at;`,
		p.OrderID,
		p.CustomerID,
		p.Status,
		p.TotalAmount,
		p.Currency,
	).Scan(
		&p.ID,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
}

func (r *PaymentRepository) Update(ctx context.Context, p *model.Payment) error {
	q := r.querier(ctx)

	_, err := q.Exec(ctx,
		`UPDATE payments SET
		order_id = $2, customer_id = $3, status = $4, total_amount = $5, currency = $6, updated_at = now()
		WHERE id = $1
		`,
		p.ID,
		p.OrderID,
		p.CustomerID,
		p.Status,
		p.TotalAmount,
		p.Currency,
	)

	return err
}

func (r *PaymentRepository) GetByID(ctx context.Context, id int64) (*model.Payment, error) {
	q := r.querier(ctx)
	p := new(model.Payment)

	err := q.QueryRow(ctx,
		`SELECT id, 
       order_id, 
       customer_id, 
       status, 
       total_amount, 
       currency, 
       created_at,
       updated_at
       FROM payments WHERE id = $1`,
		id,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.CustomerID,
		&p.Status,
		&p.TotalAmount,
		&p.Currency,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	return p, err
}

// GetByOrderID returns the most recent payment attempt of an order, whatever
// its status: callers (status polling, the saga) need to see failures too.
func (r *PaymentRepository) GetByOrderID(ctx context.Context, orderID int64) (*model.Payment, error) {
	q := r.querier(ctx)
	p := new(model.Payment)

	err := q.QueryRow(ctx,
		`SELECT id,
       order_id,
       customer_id,
       status,
       total_amount,
       currency,
       created_at,
       updated_at
       FROM payments WHERE order_id = $1 ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.CustomerID,
		&p.Status,
		&p.TotalAmount,
		&p.Currency,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	return p, err
}

// GetLiveByOrderID returns the payment that may still change the outcome of the
// order (pending or successful). It backs the idempotency check of the
// order.created handler and matches the idx_payments_live_order partial index.
func (r *PaymentRepository) GetLiveByOrderID(ctx context.Context, orderID int64) (*model.Payment, error) {
	q := r.querier(ctx)
	p := new(model.Payment)

	err := q.QueryRow(ctx,
		`SELECT id,
       order_id,
       customer_id,
       status,
       total_amount,
       currency,
       created_at,
       updated_at
       FROM payments
       WHERE order_id = $1 AND status IN ('pending', 'success')
       ORDER BY id DESC LIMIT 1`,
		orderID,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.CustomerID,
		&p.Status,
		&p.TotalAmount,
		&p.Currency,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	return p, err
}

// GetByIDForUpdate locks the payment for the duration of a transaction, so two
// webhook deliveries for the same payment cannot both write an outbox event.
func (r *PaymentRepository) GetByIDForUpdate(ctx context.Context, id int64) (*model.Payment, error) {
	q := r.querier(ctx)
	p := new(model.Payment)

	err := q.QueryRow(ctx,
		`SELECT id,
       order_id,
       customer_id,
       status,
       total_amount,
       currency,
       created_at,
       updated_at
       FROM payments WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.CustomerID,
		&p.Status,
		&p.TotalAmount,
		&p.Currency,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	return p, err
}

func (r *PaymentRepository) Delete(ctx context.Context, id int64) error {
	q := r.querier(ctx)
	_, err := q.Exec(ctx, `DELETE FROM payments WHERE id = $1`, id)
	return err
}
