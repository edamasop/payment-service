package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"payment-service/internal/model"
	"payment-service/internal/repository"
	"payment-service/internal/schema"
	"strconv"

	"github.com/sirupsen/logrus"
)

type PaymentService struct {
	paymentRepository        repository.Payment
	outboxRepository         repository.Outbox
	log                      *logrus.Entry
	client                   *http.Client
	paymentServiceWebhookURL string
	paymentGatewayURL        string
}

func NewPaymentService(
	paymentRepository repository.Payment,
	outboxRepository repository.Outbox,
	log *logrus.Entry,
	paymentServiceWebhookURL string,
	paymentGatewayURL string,
) *PaymentService {
	return &PaymentService{
		paymentRepository:        paymentRepository,
		outboxRepository:         outboxRepository,
		paymentServiceWebhookURL: paymentServiceWebhookURL,
		paymentGatewayURL:        paymentGatewayURL,
		client:                   &http.Client{},
		log:                      log.WithFields(logrus.Fields{"service": "payment_service"}),
	}
}

func (p *PaymentService) InitializePayment(
	ctx context.Context,
	req schema.CreatePayment,
) (*schema.PaymentResult, error) {
	payment := new(model.Payment)
	payment.CustomerID = req.CustomerID
	payment.OrderID = req.OrderID
	payment.TotalAmount = req.Amount
	payment.Currency = req.Currency
	payment.Status = model.PaymentPending // Step 1: Always start with Pending status

	// Step 2: Persist the initial payment record to generate payment.ID in DB
	err := p.paymentRepository.Create(ctx, payment)
	if err != nil {
		p.log.Errorf("Error on creating payment record: %v", err)
		return nil, fmt.Errorf("failed to initialize payment in db: %w", err)
	}

	// Step 3: Send request to the external payment gateway.
	// We pass payment.ID as a reference/idempotency key.
	gatewayResp, err := p.gatewayPaymentRequest(ctx, payment.ID, req)
	if err != nil {
		p.log.Errorf("Network error from gateway for payment ID %d: %v", payment.ID, err)

		// CRITICAL: Network timeout is NOT a failed payment.
		// We safely return the current 'Pending' state. The background worker/cron or webhook will sync it later.
		return &schema.PaymentResult{
			PaymentID:   payment.ID,
			OrderID:     payment.OrderID,
			CustomerID:  payment.CustomerID,
			Status:      string(model.PaymentPending),
			RedirectURL: p.paymentGatewayURL, // Fallback URL
		}, nil
	}

	// Step 4: Handle immediate rejection response if the gateway evaluated it synchronously
	if gatewayResp.IsRejected {
		payment.Status = model.PaymentFailed
		if updateErr := p.paymentRepository.Update(ctx, payment); updateErr != nil {
			p.log.Errorf("Failed to update payment status to FAILED: %v", updateErr)
		}
	}

	// Step 5: Build the final response with the dynamic redirect URL provided by the gateway (e.g., 3D Secure link)
	res := new(schema.PaymentResult)
	res.PaymentID = payment.ID
	res.OrderID = payment.OrderID
	res.CustomerID = payment.CustomerID
	res.Status = string(payment.Status)
	res.RedirectURL = gatewayResp.RedirectURL

	return res, nil
}

// gatewayPaymentRequest sends an HTTP POST request to the external payment system.
// It returns a generic response containing the redirect URL and structural transaction status.
func (p *PaymentService) gatewayPaymentRequest(ctx context.Context, paymentID int64, req schema.CreatePayment) (*schema.GatewayResponse, error) {
	body := new(schema.CreateGatewayPayment)
	body.Amount = req.Amount
	body.OrderID = strconv.FormatInt(req.OrderID, 10)
	body.Currency = req.Currency
	body.WebhookURL = p.paymentServiceWebhookURL

	// Pass our internal payment ID to map it securely during webhook processing
	body.PaymentID = strconv.FormatInt(paymentID, 10)

	payload, err := json.Marshal(body)
	if err != nil {
		p.log.Errorf("Error on marshalling gateway payload: %v", err)
		return nil, err
	}

	// Always use HTTP requests with context to prevent un-cancelable, hanging goroutines
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.paymentGatewayURL, bytes.NewBuffer(payload))
	if err != nil {
		p.log.Errorf("Error creating http request object: %v", err)
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Standardizing Idempotency protection at the gateway protocol layer
	httpReq.Header.Set("X-Idempotency-Key", strconv.FormatInt(paymentID, 10))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.log.Errorf("Error on posting to gateway: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("gateway returned unexpected status code: %s", resp.Status)
	}

	// Decode the gateway API payload response
	var gatewayResp schema.GatewayResponse
	if err := json.NewDecoder(resp.Body).Decode(&gatewayResp); err != nil {
		p.log.Errorf("Error decoding gateway response body: %v", err)
		return nil, err
	}

	return &gatewayResp, nil
}

func (p *PaymentService) ProcessWebhook(
	ctx context.Context,
	params schema.WebhookParams,
) error {
	// TODO: Implement safe webhook processing
	// 1. Verify the webhook signature (HMAC) to ensure it came from the actual provider
	// 2. Fetch payment by ID from DB
	// 3. Update payment status safely (e.g., PENDING -> SUCCESS/FAILED) using idempotent state machine
	// 4. Save to Outbox table if you need to dispatch event to an Order service asynchronously
	fmt.Printf("Processing webhook:%v\n\n", params)
	return nil
}

func (p *PaymentService) GetStatusByOrderID(
	ctx context.Context,
	orderID string,
) (*schema.PaymentStatusInfo, error) {
	// TODO: Implement order status polling
	// This will be called by your client-side frontend or background reconciliation worker
	return nil, nil
}
