package http

import (
	"net/http"
	v1 "payment-service/internal/delivery/http/v1"
)

func NewRouter(handlers *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	v1.RegisterPaymentRoutes(mux, handlers.Payment)

	return mux
}
