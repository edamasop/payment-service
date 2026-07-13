package v1

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"payment-service/internal/schema"
	"payment-service/internal/service"
)

const (
	webhookSecret = "my-secret-key"
)

type PaymentHandler struct {
	paymentService service.Payment
}

func NewPaymentHandler(paymentService service.Payment) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
	}
}

func (h *PaymentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req schema.CreatePayment
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
		return
	}

	res, err := h.paymentService.InitializePayment(r.Context(), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}

func (h *PaymentHandler) Get(w http.ResponseWriter, r *http.Request) {}

func (h *PaymentHandler) List(w http.ResponseWriter, r *http.Request) {}

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	receivedSignature := r.Header.Get("X-Signature")

	hash := hmac.New(sha256.New, []byte(webhookSecret))
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
		return
	}

	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	hash.Write(bodyBytes)
	expectedSignature := hex.EncodeToString(hash.Sum(nil))

	if !hmac.Equal([]byte(receivedSignature), []byte(expectedSignature)) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "signatures do not match"})
		return
	}

	var req schema.WebhookParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
		return
	}

	err = h.paymentService.ProcessWebhook(r.Context(), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
}
