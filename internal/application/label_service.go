package application

import (
	"context"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type LabelService struct {
	Labels *store.LabelStore
}

func (s *LabelService) Create(
	ctx context.Context,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	return s.Labels.CreateWithOperation(ctx, name, actor)
}

func (s *LabelService) Rename(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	return s.Labels.RenameVersionedWithOperation(
		ctx, id, expectedVersion, name, actor,
	)
}

func (s *LabelService) Delete(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	actor domain.OperationActor,
) error {
	return s.Labels.DeleteVersionedWithOperation(ctx, id, expectedVersion, actor)
}
