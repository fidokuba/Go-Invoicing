package admin

import (
	"context"

	"github.com/google/uuid"
)

// OrganisationRepository describes how organisations are read from and written
// to storage. It deliberately says nothing about PostgreSQL, HTTP or JSON, so a
// service depending on this interface can be tested against a fake and the
// storage engine can change without the service noticing.
//
// Every method takes a context.Context as its first argument so that a caller
// can cancel a slow query when, for example, the HTTP request behind it is
// abandoned.
type OrganisationRepository interface {
	Create(ctx context.Context, organisation *Organisation) error
	GetByID(ctx context.Context, id uuid.UUID) (*Organisation, error)
}
