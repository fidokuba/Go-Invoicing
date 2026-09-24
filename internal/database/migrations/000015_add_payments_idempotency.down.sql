DROP INDEX payments_invoice_idempotency_key_unique;

ALTER TABLE payments
    DROP CONSTRAINT payments_request_hash_length_check,
    DROP CONSTRAINT payments_idempotency_key_length_check,
    DROP CONSTRAINT payments_idempotency_pair_check;

ALTER TABLE payments
    DROP COLUMN request_hash,
    DROP COLUMN idempotency_key;
