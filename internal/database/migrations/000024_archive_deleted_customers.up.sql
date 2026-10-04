-- Customers are no longer deleted, only archived (status = 'archived';
-- see customer.CustomerService.SetStatus). Any customer soft-deleted by
-- the short-lived DELETE /customers/{id} becomes an Archived customer
-- instead, so it reappears under the Archived filter rather than staying
-- invisible with no way back.
UPDATE customers
SET status = 'archived',
    deleted_at = NULL,
    updated_at = NOW()
WHERE deleted_at IS NOT NULL;
