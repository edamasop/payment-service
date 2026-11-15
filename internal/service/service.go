package service

import (
	"context"
	"payment-service/internal/config"
	"payment-service/internal/repository"
	"payment-service/internal/schema"

	"github.com/sirupsen/logrus"
)

type Payment interface {
	InitializePayment(ctx context.Context, payment schema.CreatePayment) (*schema.PaymentResult, error)

	ProcessWebhook(ctx context.Context, params schema.WebhookParams) error

	GetStatusByOrderID(ctx context.Context, orderID string) (*schema.PaymentStatusInfo, error)
}

type Services struct {
	Payment Payment
}

func NewServices(cfg *config.Config, repos *repository.Repositories, log *logrus.Logger) *Services {

	return &Services{
		Payment: NewPaymentService(
			repos.Payment,
			repos.Outbox,
			repos.TxManager,
			logrus.NewEntry(log),
			cfg.PaymentWebhookURL,
			cfg.PaymentGatewayURL,
		),
	}
}
