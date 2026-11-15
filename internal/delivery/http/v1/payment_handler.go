package v1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"payment-service/internal/schema"
	"payment-service/internal/service"
)

const maxRequestBodyLen = 1 << 20 // 1 MiB

type PaymentHandler struct {
	paymentService service.Payment
	webhookSecret  []byte
}

// NewPaymentHandler takes the HMAC key from configuration: a secret compiled
// into the binary is the same secret in every environment and in the repo.
func NewPaymentHandler(paymentService service.Payment, webhookSecret string) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		webhookSecret:  []byte(webhookSecret),
	}
}

func (h *PaymentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req schema.CreatePayment
	if !decodeJSON(w, r, &req) {
		return
	}

	res, err := h.paymentService.InitializePayment(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, res)
}

// Get returns the payment status of one order, which is what a frontend polls
// after the user returns from the gateway's checkout page.
func (h *PaymentHandler) Get(w http.ResponseWriter, r *http.Request) {
	status, err := h.paymentService.GetStatusByOrderID(r.Context(), r.PathValue("order_id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// List is not implemented yet.
func (h *PaymentHandler) List(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "payment listing is not implemented yet")
}

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyLen))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The signature covers the exact bytes the gateway signed, so the body is
	// read once and verified before any decoding happens.
	mac := hmac.New(sha256.New, h.webhookSecret)
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(expectedSignature)) {
		writeError(w, http.StatusUnauthorized, "signatures do not match")
		return
	}

	var params schema.WebhookParams
	if err := json.Unmarshal(body, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.paymentService.ProcessWebhook(r.Context(), params); err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *PaymentHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidArgument), errors.Is(err, service.ErrUnsupportedStatus):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrPaymentNotFound):
		writeError(w, http.StatusNotFound, "payment not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyLen)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
