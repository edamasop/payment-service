package http

import v1 "payment-service/internal/delivery/http/v1"

type Handlers struct {
	Payment *v1.PaymentHandler
}

func NewHandlers() *Handlers {
	return &Handlers{
		Payment: v1.NewPaymentHandler(),
	}
}
