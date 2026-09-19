package customer

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrBillingAddressNotFound is returned both when a customer has no
// billing address yet and when the given customer doesn't exist (or
// doesn't belong to the given organisation) at all — the two cases are
// indistinguishable from a single query and, by the same convention
// ErrInvoiceNotFound already uses for "doesn't exist" vs "wrong tenant",
// deliberately collapsed into one error and one HTTP response. This
// avoids ever revealing that a foreign organisation's customer exists.
var ErrBillingAddressNotFound = errors.New("billing address not found")

// AddressRepository describes how a customer's billing address is read
// from and written to storage. There is exactly one address concept in
// this milestone (billing) and exactly one row per customer, so this is
// deliberately not a general address CRUD interface — see
// UpsertBillingAddress.
//
// Every method takes organisationID explicitly and enforces it in its own
// SQL via a join back to customers — addresses has no organisation_id
// column of its own, the same situation payments has relative to
// invoices, and the same defense-in-depth reasoning applies: a caller
// cannot reach another organisation's customer's address by knowing its
// customer ID, even if the service layer's own check were ever bypassed
// or forgotten.
type AddressRepository interface {
	// WithTx returns a repository whose operations run against the
	// supplied transaction instead of the default connection pool
	// (Milestone 7 Part 2) — needed so InvoiceService.Send can read a
	// customer's billing address for its snapshot within the same
	// transaction that locks and finalises the invoice. The repository
	// itself never calls Begin, Commit or Rollback.
	WithTx(tx pgx.Tx) AddressRepository

	// GetBillingAddressByCustomerID returns customerID's billing address.
	// Returns ErrBillingAddressNotFound if the customer doesn't exist,
	// doesn't belong to organisationID, or simply has no billing address
	// yet.
	GetBillingAddressByCustomerID(ctx context.Context, organisationID uuid.UUID, customerID uuid.UUID) (*Address, error)

	// UpsertBillingAddress creates customerID's billing address if none
	// exists yet, or replaces its fields if one already does — see the
	// Postgres implementation for how a single INSERT ... ON CONFLICT
	// statement does both without ever creating a second row for the same
	// customer. address.ID is only used if a new row is created; on an
	// update the existing row's ID is preserved and returned. Returns
	// ErrBillingAddressNotFound if customerID doesn't exist or doesn't
	// belong to organisationID.
	UpsertBillingAddress(ctx context.Context, organisationID uuid.UUID, customerID uuid.UUID, address *Address) (*Address, error)
}
