package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrOrganisationNameRequired is returned when Create is called with an
// empty (or whitespace-only) name. Handlers can use errors.Is to turn this
// into a 400 rather than a generic 500.
var ErrOrganisationNameRequired = errors.New("organisation name is required")

// OrganisationService sits between the HTTP layer and the repository. It
// depends on the OrganisationRepository interface, not on any concrete
// implementation, so it doesn't know or care that organisations happen to
// live in PostgreSQL today.
type OrganisationService struct {
	repository OrganisationRepository
}

func NewOrganisationService(
	repository OrganisationRepository,
) *OrganisationService {
	return &OrganisationService{
		repository: repository,
	}
}

// Create validates the requested name, generates the organisation's ID, and
// persists it. The caller supplies only what a client is allowed to
// specify — the service, not the client, decides the ID.
func (s *OrganisationService) Create(
	ctx context.Context,
	name string,
) (*Organisation, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrOrganisationNameRequired
	}

	organisation := &Organisation{
		ID:   uuid.New(),
		Name: name,
	}

	if err := s.repository.Create(ctx, organisation); err != nil {
		return nil, err
	}

	return organisation, nil
}

func (s *OrganisationService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*Organisation, error) {
	return s.repository.GetByID(ctx, id)
}
