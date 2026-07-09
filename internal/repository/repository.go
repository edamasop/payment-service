package repository

import (
	"context"
	"payment-service/internal/model"
)

type Payment interface {
	Create(ctx context.Context, p *model.Payment) error
	Update(ctx context.Context, p *model.Payment) error
	GetByID(ctx context.Context, id int64) (*model.Payment, error)
	Delete(ctx context.Context, id int64) error
}

type Outbox interface {
	Create(ctx context.Context, event *model.OutboxEvent) error
	GetByID(ctx context.Context, id int64) (*model.OutboxEvent, error)
	Update(ctx context.Context, event *model.OutboxEvent) error
	Delete(ctx context.Context, id int64) error

	GetUnpublished(ctx context.Context, limit int) ([]*model.OutboxEvent, error)
	MarkPublished(ctx context.Context, id int64) error
}

type Repositories struct {
	Payment Payment
	Outbox  Outbox
}

func NewRepositories() *Repositories {
	return &Repositories{}
}
