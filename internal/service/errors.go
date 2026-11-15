package service

import "errors"

var (
	// ErrInvalidArgument marks a request that can never succeed, so Kafka
	// consumers must not retry it.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrPaymentNotFound means the referenced payment does not exist.
	ErrPaymentNotFound = errors.New("payment not found")

	// ErrUnsupportedStatus is returned when the gateway reports a status this
	// service has no mapping for.
	ErrUnsupportedStatus = errors.New("unsupported payment status")
)
