package v1

import "net/http"

func RegisterPaymentRoutes(
	mux *http.ServeMux,
	handler *PaymentHandler,
) {
	mux.HandleFunc("POST /v1/payments", handler.Create)
	mux.HandleFunc("POST /v1/payments/webhook", handler.Webhook)
	mux.HandleFunc("GET /v1/payments", handler.List)
	mux.HandleFunc("GET /v1/payments/orders/{order_id}", handler.Get)
}
