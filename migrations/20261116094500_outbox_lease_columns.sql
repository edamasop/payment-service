-- +goose Up
-- +goose StatementBegin
-- ClaimUnpublished() leases rows through these columns, but the original table
-- definition never had them: payment lookups worked, while every webhook that
-- wrote to the outbox failed with "column locked_until does not exist".
ALTER TABLE payment_outbox
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;

-- Mirrors the ClaimUnpublished predicate (unpublished rows, insertion order).
CREATE INDEX IF NOT EXISTS idx_payment_outbox_claim
    ON payment_outbox (created_at, id)
    WHERE published = FALSE;

-- At most one live payment per order. This is what makes the order.created
-- handler idempotent when Kafka redelivers the event, and it still allows a
-- retry after a failed or cancelled attempt.
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_live_order
    ON payments (order_id)
    WHERE status IN ('pending', 'success');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_payments_live_order;
DROP INDEX IF EXISTS idx_payment_outbox_claim;

ALTER TABLE payment_outbox
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS locked_until;
-- +goose StatementEnd
