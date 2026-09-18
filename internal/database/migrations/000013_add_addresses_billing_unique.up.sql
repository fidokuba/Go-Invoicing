--Milestone 7 Part 1: a customer's billing address is a single, upsertable
--resource (PUT /customers/{id}/billing-address). Without this constraint,
--two concurrent PUTs could each fail to find an existing row and both
--INSERT, leaving two "billing" rows for the same customer with no way to
--know which one is authoritative. The partial index enforces at most one
--type = 'billing' row per customer, and also serves as the ON CONFLICT
--target for the upsert itself.
CREATE UNIQUE INDEX addresses_customer_billing_unique
    ON addresses (customer_id)
    WHERE type = 'billing';
