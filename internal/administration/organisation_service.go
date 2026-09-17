package admin

import (
	"context"

	"github.com/google/uuid"
)

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

// Create is currently a thin pass-through. This is where validation,
// business rules and related-record initialisation will land once we need
// them.
func (s *OrganisationService) Create(
	ctx context.Context,
	organisation *Organisation,
) error {
	return s.repository.Create(ctx, organisation)
}

func (s *OrganisationService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*Organisation, error) {
	return s.repository.GetByID(ctx, id)
}
