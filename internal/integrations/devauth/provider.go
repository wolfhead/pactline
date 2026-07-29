package devauth

import (
	"context"
	"errors"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

type SessionIssuer interface {
	IssueSession(ctx context.Context, userID uuid.UUID, requestID string) (identity.SessionTokens, error)
}

type UserLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

type LoginRequest struct {
	UserID uuid.UUID `json:"user_id"`
}

type Provider struct {
	users    UserLookup
	sessions SessionIssuer
}

func New(users UserLookup, sessions SessionIssuer) *Provider {
	return &Provider{users: users, sessions: sessions}
}

// Authenticate is deliberately local-account authentication, not an
// imitation of Lark OAuth. The resulting tokens belong to the normal
// application session mechanism.
func (p *Provider) Authenticate(ctx context.Context, userID uuid.UUID, requestID string) (identity.SessionTokens, error) {
	user, err := p.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return identity.SessionTokens{}, identity.ErrSessionInvalid
		}
		return identity.SessionTokens{}, err
	}
	if !user.Active {
		return identity.SessionTokens{}, identity.ErrUserInactive
	}
	return p.sessions.IssueSession(ctx, userID, requestID)
}
