package service

import (
	"context"
	"fmt"
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

	paymentWebhookURL := fmt.Sprintf("http://localhost:%v/v1/payments/webhook", cfg.Port)

	return &Services{
		Payment: NewPaymentService(
			repos.Payment,
			repos.Outbox,
			repos.TxManager,
			logrus.NewEntry(log),
			paymentWebhookURL,
			cfg.PaymentGatewayURL,
		),
	}
}
