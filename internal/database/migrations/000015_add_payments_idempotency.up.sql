--Milestone 13 Part 1: payment creation (POST /invoices/{id}/payments) is
--made safely retryable via a client-supplied Idempotency-Key. The
--idempotency identity lives directly on the successfully created payment
--row - there is deliberately no separate idempotency table, no
--"in progress" state and no expiry: a key only ever becomes durable in
--the same transaction (and the same row) as the payment it produced, so
--the two can never disagree, and it is retained exactly as long as the
--payment itself.
--
--idempotency_key is the client's opaque key; request_hash is the
--SHA-256 fingerprint of the logical payment request it was first used
--with (see internal/invoice/payment_idempotency.go). Both are nullable
--only because payments recorded before this migration have neither -
--those rows are deliberately left NULL, never backfilled with invented
--keys. Every payment created from this point on has both.
ALTER TABLE payments
    ADD COLUMN idempotency_key TEXT,
    ADD COLUMN request_hash BYTEA;

ALTER TABLE payments
    ADD CONSTRAINT payments_idempotency_pair_check
        CHECK ((idempotency_key IS NULL) = (request_hash IS NULL)),
    ADD CONSTRAINT payments_idempotency_key_length_check
        CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 16 AND 128),
    ADD CONSTRAINT payments_request_hash_length_check
        CHECK (request_hash IS NULL OR octet_length(request_hash) = 32);

--A key is scoped to one invoice (whose tenant ownership InvoiceService
--has already established before ever looking the key up). This index is
--the database-level invariant/backstop and the lookup path - the primary
--concurrency control remains the invoice row's FOR UPDATE lock, which
--already serialises every payment against the same invoice.
CREATE UNIQUE INDEX payments_invoice_idempotency_key_unique
    ON payments (invoice_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
