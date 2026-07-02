package v1

import (
	"net/http"
)

type PaymentHandler struct {
}

func NewPaymentHandler() *PaymentHandler {
	return &PaymentHandler{}
}

func (h *PaymentHandler) Create(w http.ResponseWriter, r *http.Request) {

}

func (h *PaymentHandler) Get(w http.ResponseWriter, r *http.Request) {}

func (h *PaymentHandler) List(w http.ResponseWriter, r *http.Request) {}

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {}
