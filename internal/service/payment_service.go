package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"payment-service/internal/model"
	"payment-service/internal/repository"
	"payment-service/internal/schema"
	"strconv"

	"github.com/edamasop/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sirupsen/logrus"
)

type PaymentService struct {
	paymentRepository        repository.Payment
	outboxRepository         repository.Outbox
	txManager                repository.TxManager
	log                      *logrus.Entry
	client                   *http.Client
	paymentServiceWebhookURL string
	paymentGatewayURL        string
}

func NewPaymentService(
	paymentRepository repository.Payment,
	outboxRepository repository.Outbox,
	txManager repository.TxManager,
	log *logrus.Entry,
	paymentServiceWebhookURL string,
	paymentGatewayURL string,
) *PaymentService {
	return &PaymentService{
		paymentRepository:        paymentRepository,
		outboxRepository:         outboxRepository,
		txManager:                txManager,
		paymentServiceWebhookURL: paymentServiceWebhookURL,
		paymentGatewayURL:        paymentGatewayURL,
		client:                   &http.Client{},
		log:                      log.WithFields(logrus.Fields{"service": "payment_service"}),
	}
}

// InitializePayment creates a payment and asks the gateway for a checkout
// session. It is idempotent per order: a redelivered order.created event, or a
// double-clicked checkout button, returns the payment that already exists
// instead of charging the customer twice.
func (p *PaymentService) InitializePayment(
	ctx context.Context,
	req schema.CreatePayment,
) (*schema.PaymentResult, error) {
	if req.OrderID <= 0 || req.CustomerID <= 0 {
		return nil, fmt.Errorf("%w: order_id and customer_id must be positive", ErrInvalidArgument)
	}

	existing, err := p.paymentRepository.GetLiveByOrderID(ctx, req.OrderID)
	switch {
	case err == nil:
		p.log.WithFields(logrus.Fields{
			"order_id":   req.OrderID,
			"payment_id": existing.ID,
			"status":     existing.Status,
		}).Info("payment already exists for order; reusing it")

		return p.paymentResult(existing, p.pendingRedirectURL(existing)), nil
	case !errors.Is(err, pgx.ErrNoRows):
		p.log.WithError(err).WithField("order_id", req.OrderID).Error("failed to look up existing payment")
		return nil, fmt.Errorf("look up live payment for order %d: %w", req.OrderID, err)
	}

	payment := &model.Payment{
		OrderID:     req.OrderID,
		CustomerID:  req.CustomerID,
		TotalAmount: req.Amount,
		Currency:    req.Currency,
		Status:      model.PaymentPending, // Step 1: always start as pending
	}

	// Step 2: persist the initial record to obtain payment.ID from the database.
	if err := p.paymentRepository.Create(ctx, payment); err != nil {
		if isUniqueViolation(err) {
			// Lost the race against a concurrent delivery of the same event:
			// the other one won, so return its payment.
			if live, getErr := p.paymentRepository.GetLiveByOrderID(ctx, req.OrderID); getErr == nil {
				p.log.WithFields(logrus.Fields{
					"order_id":   req.OrderID,
					"payment_id": live.ID,
				}).Info("concurrent payment creation detected; reusing the existing payment")

				return p.paymentResult(live, p.pendingRedirectURL(live)), nil
			}
		}

		p.log.WithError(err).WithField("order_id", req.OrderID).Error("failed to create payment record")
		return nil, fmt.Errorf("failed to initialize payment in db: %w", err)
	}

	// Step 3: ask the gateway for a checkout session. The payment id doubles as
	// the idempotency key.
	gatewayResp, err := p.gatewayPaymentRequest(ctx, payment.ID, req)
	if err != nil {
		p.log.WithError(err).WithFields(logrus.Fields{
			"payment_id": payment.ID,
			"order_id":   payment.OrderID,
		}).Warn("gateway request failed; keeping payment pending until a webhook or reconciliation arrives")

		// A network timeout is NOT a failed payment: keep pending and let the
		// webhook (or a later reconciliation) settle it.
		return p.paymentResult(payment, p.paymentGatewayURL), nil
	}

	// Step 4: handle an immediate rejection reported synchronously.
	if gatewayResp.IsRejected {
		payment.Status = model.PaymentFailed
		if updateErr := p.paymentRepository.Update(ctx, payment); updateErr != nil {
			p.log.WithError(updateErr).WithField("payment_id", payment.ID).Error("failed to mark payment as failed")
		} else {
			p.log.WithFields(logrus.Fields{
				"payment_id": payment.ID,
				"reason":     gatewayResp.ErrorMessage,
			}).Warn("gateway rejected the payment synchronously")
		}
	}

	return p.paymentResult(payment, gatewayResp.RedirectURL), nil
}

func (p *PaymentService) paymentResult(payment *model.Payment, redirectURL string) *schema.PaymentResult {
	return &schema.PaymentResult{
		PaymentID:   payment.ID,
		OrderID:     payment.OrderID,
		CustomerID:  payment.CustomerID,
		Status:      string(payment.Status),
		RedirectURL: redirectURL,
	}
}

// pendingRedirectURL only exposes the gateway URL while the payment can still
// be completed; a finished payment has no checkout page to send the user to.
func (p *PaymentService) pendingRedirectURL(payment *model.Payment) string {
	if payment.Status == model.PaymentPending {
		return p.paymentGatewayURL
	}

	return ""
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
		p.log.WithError(err).Error("failed to marshal gateway payload")
		return nil, err
	}

	// Always use HTTP requests with context to prevent un-cancelable, hanging goroutines
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.paymentGatewayURL, bytes.NewBuffer(payload))
	if err != nil {
		p.log.WithError(err).Error("failed to build gateway request")
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Standardizing Idempotency protection at the gateway protocol layer
	httpReq.Header.Set("X-Idempotency-Key", strconv.FormatInt(paymentID, 10))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.log.WithError(err).Error("failed to post to gateway")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("gateway returned unexpected status code: %s", resp.Status)
	}

	// Decode the gateway API payload response
	var gatewayResp schema.GatewayResponse
	if err := json.NewDecoder(resp.Body).Decode(&gatewayResp); err != nil {
		p.log.WithError(err).Error("failed to decode gateway response body")
		return nil, err
	}

	return &gatewayResp, nil
}

// ProcessWebhook applies an asynchronous gateway notification.
//
// The gateway retries notifications and may deliver them more than once, so the
// whole update is idempotent: repeating the status that is already stored does
// nothing, and a late failure never overwrites a successful payment.
func (p *PaymentService) ProcessWebhook(
	ctx context.Context,
	params schema.WebhookParams,
) error {
	id, err := strconv.ParseInt(params.PaymentID, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: payment_id %q is not a number", ErrInvalidArgument, params.PaymentID)
	}

	status, eventType, ok := paymentStatusFromGateway(params.Status)
	if !ok {
		return fmt.Errorf("%w: gateway reported %q", ErrUnsupportedStatus, params.Status)
	}

	err = p.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		payment, err := p.paymentRepository.GetByIDForUpdate(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrPaymentNotFound
			}

			p.log.WithError(err).WithField("payment_id", id).Error("failed to load payment")
			return fmt.Errorf("get payment %d: %w", id, err)
		}

		if payment.Status == status {
			p.log.WithFields(logrus.Fields{
				"payment_id": payment.ID,
				"status":     status,
			}).Info("webhook ignored: payment already has this status")
			return nil
		}

		if payment.Status == model.PaymentSuccess && status == model.PaymentFailed {
			p.log.WithFields(logrus.Fields{
				"payment_id": payment.ID,
				"status":     payment.Status,
				"incoming":   status,
			}).Warn("ignoring failed webhook for an already successful payment")
			return nil
		}

		payment.Status = status

		payload, err := json.Marshal(events.PaymentPayload{
			ID:          payment.ID,
			OrderID:     payment.OrderID,
			CustomerID:  payment.CustomerID,
			Status:      string(payment.Status),
			TotalAmount: payment.TotalAmount,
			Currency:    payment.Currency,
		})
		if err != nil {
			p.log.WithError(err).Error("failed to marshal payment event payload")
			return fmt.Errorf("marshal payment event payload: %w", err)
		}

		if err := p.paymentRepository.Update(ctx, payment); err != nil {
			p.log.WithError(err).WithField("payment_id", payment.ID).Error("failed to update payment record")
			return fmt.Errorf("update payment %d: %w", payment.ID, err)
		}

		if err := p.outboxRepository.Create(ctx, &model.OutboxEvent{
			OrderID:    payment.OrderID,
			CustomerID: payment.CustomerID,
			EventType:  string(eventType),
			Payload:    payload,
		}); err != nil {
			p.log.WithError(err).WithField("payment_id", payment.ID).Error("failed to create outbox record")
			return fmt.Errorf("create payment outbox event: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	p.log.WithFields(logrus.Fields{
		"payment_id": id,
		"status":     status,
	}).Info("payment status updated from webhook")

	return nil
}

func (p *PaymentService) GetStatusByOrderID(
	ctx context.Context,
	orderID string,
) (*schema.PaymentStatusInfo, error) {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("%w: order_id must be a positive number", ErrInvalidArgument)
	}

	payment, err := p.paymentRepository.GetByOrderID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}

		p.log.WithError(err).WithField("order_id", id).Error("failed to load payment by order")
		return nil, fmt.Errorf("get payment for order %d: %w", id, err)
	}

	return &schema.PaymentStatusInfo{
		CustomerID: payment.CustomerID,
		OrderID:    strconv.FormatInt(payment.OrderID, 10),
		Status:     string(payment.Status),
		UpdatedAt:  payment.UpdatedAt,
	}, nil
}

// paymentStatusFromGateway maps a gateway status onto our own state and the
// event that announces it.
func paymentStatusFromGateway(gatewayStatus string) (model.PaymentStatus, events.EventType, bool) {
	switch gatewayStatus {
	case schema.PaymentGatewayStatusSuccessful:
		return model.PaymentSuccess, events.PaymentStatusSuccessful, true
	case schema.PaymentGatewayStatusFailed:
		return model.PaymentFailed, events.PaymentStatusFailed, true
	default:
		return "", "", false
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
