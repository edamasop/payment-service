package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type PaymentStatus string

const (
	PaymentSuccess   PaymentStatus = "success"
	PaymentPending   PaymentStatus = "pending"
	PaymentFailed    PaymentStatus = "failed"
	PaymentCancelled PaymentStatus = "cancelled"
)

type Payment struct {
	ID          int64
	OrderID     int64
	CustomerID  int64
	Status      PaymentStatus
	TotalAmount decimal.Decimal
	Currency    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
