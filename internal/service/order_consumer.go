package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/edamasop/events"
	"github.com/sirupsen/logrus"

	"payment-service/internal/schema"
)

// orderEventTimeout bounds one payment created from a Kafka event. The handler
// signature carries no context, so a stuck database or gateway call is cut off
// here instead of blocking a partition worker forever.
const orderEventTimeout = 15 * time.Second

// OrderConsumer starts a payment when an order is created, so the saga does not
// depend on a client remembering to call POST /v1/payments.
type OrderConsumer struct {
	payments Payment
	log      *logrus.Entry
}

func NewOrderConsumer(payments Payment, log *logrus.Entry) *OrderConsumer {
	return &OrderConsumer{
		payments: payments,
		log:      log.WithField("consumer", "order-events"),
	}
}

// HandleOrderCreated creates (or reuses) the payment for a new order.
func (c *OrderConsumer) HandleOrderCreated(data json.RawMessage) (bool, error) {
	var order events.OrderPayload

	if err := json.Unmarshal(data, &order); err != nil {
		c.log.WithError(err).Warn("invalid order.created payload")
		return false, fmt.Errorf("decode order.created payload: %w", err)
	}

	if order.ID <= 0 || order.CustomerID <= 0 || !order.TotalAmount.IsPositive() {
		c.log.WithFields(logrus.Fields{
			"order_id":    order.ID,
			"customer_id": order.CustomerID,
			"amount":      order.TotalAmount.String(),
		}).Warn("order.created payload cannot be charged; acknowledged without retry")

		return false, fmt.Errorf(
			"%w: order %d has invalid customer or amount",
			ErrInvalidArgument,
			order.ID,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), orderEventTimeout)
	defer cancel()

	result, err := c.payments.InitializePayment(ctx, schema.CreatePayment{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		Currency:   order.Currency,
		Amount:     order.TotalAmount,
	})
	if err != nil {
		// A business rejection cannot succeed on retry; anything else (database
		// down, gateway unreachable) is worth another attempt.
		if errors.Is(err, ErrInvalidArgument) {
			c.log.WithError(err).WithField("order_id", order.ID).Warn("payment request rejected")
			return false, err
		}

		c.log.WithError(err).WithField("order_id", order.ID).Error("failed to initialize payment; will retry")
		return true, err
	}

	c.log.WithFields(logrus.Fields{
		"order_id":   order.ID,
		"payment_id": result.PaymentID,
		"status":     result.Status,
	}).Info("payment initialized from order.created event")

	return false, nil
}
