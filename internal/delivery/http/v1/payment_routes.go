package v1

import "net/http"

func RegisterPaymentRoutes(
	mux *http.ServeMux,
	handler *PaymentHandler,
) {
	mux.Handle("POST /v1/payments", http.HandlerFunc(handler.Create))
	mux.Handle("POST /v1/payments/webhook", http.HandlerFunc(handler.Webhook))
	mux.Handle("POST /v1/payments/orders", http.HandlerFunc(handler.List))
	mux.Handle("POST /v1/payments/orders/{order_id}", http.HandlerFunc(handler.Get))
}
