package http

import (
	v1 "payment-service/internal/delivery/http/v1"
	"payment-service/internal/service"
)

type Handlers struct {
	Payment *v1.PaymentHandler
}

func NewHandlers(services *service.Services) *Handlers {
	return &Handlers{
		Payment: v1.NewPaymentHandler(services.Payment),
	}
}
