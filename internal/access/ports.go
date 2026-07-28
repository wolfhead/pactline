package access

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	CreateToken(context.Context, Token) error
	GetToken(context.Context, uuid.UUID) (TokenWithUser, error)
	ListUserTokens(context.Context, uuid.UUID) ([]Token, error)
	RevokeToken(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	TouchToken(context.Context, uuid.UUID, time.Time, time.Time) error
}

type Clock interface {
	Now() time.Time
}

type SecretGenerator interface {
	Bytes(size int) ([]byte, error)
}
