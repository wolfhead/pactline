// Package channel defines the provider-neutral messaging boundary used by the
// first-party Agent. Provider identifiers are intentionally opaque strings.
package channel

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultContextPageSize = 20
	MaxContextPageSize     = 20
	MaxContextMessages     = 100
	MaxContextAge          = 7 * 24 * time.Hour
)

var (
	ErrInvalidEvent       = errors.New("channel event is invalid")
	ErrUnsupportedMessage = errors.New("channel message type is unsupported")
	ErrExplicitMention    = errors.New("Pactline bot was not explicitly mentioned")
	ErrContextBoundary    = errors.New("channel context request exceeds its boundary")
)

type ProviderMessageID string

type Mention struct {
	SubjectID string
	Name      string
	IsBot     bool
}

type IncomingMessage struct {
	Provider             string
	TenantID             string
	EventID              string
	ConversationID       string
	MessageID            string
	ThreadRootMessageID  string
	ReplyParentMessageID string
	SenderSubjectID      string
	MessageType          string
	Text                 string
	Mentions             []Mention
	CreatedAt            time.Time
	BotMentioned         bool
}

type ChannelMessage struct {
	MessageID       string
	Cursor          string
	SenderSubjectID string
	SenderName      string
	Text            string
	CreatedAt       time.Time
	IsBot           bool
}

type ContextRequest struct {
	TenantID         string
	ConversationID   string
	TriggerMessageID string
	BeforeCursor     string
	PageSize         int
	NotBefore        time.Time
	NotAfter         time.Time
}

func (r ContextRequest) Validate() error {
	if r.TenantID == "" || r.ConversationID == "" || r.TriggerMessageID == "" ||
		r.NotBefore.IsZero() || r.NotAfter.IsZero() || !r.NotBefore.Before(r.NotAfter) {
		return ErrContextBoundary
	}
	if r.PageSize <= 0 || r.PageSize > MaxContextPageSize {
		return ErrContextBoundary
	}
	if r.NotAfter.Sub(r.NotBefore) > MaxContextAge {
		return ErrContextBoundary
	}
	return nil
}

type ReplyRequest struct {
	TenantID        string
	ConversationID  string
	TargetMessageID string
	Body            string
	IdempotencyKey  string
}

type ChannelAdapter interface {
	NormalizeEvent(ctx context.Context, payload []byte) (IncomingMessage, error)
	FetchContext(ctx context.Context, request ContextRequest) ([]ChannelMessage, error)
	Reply(ctx context.Context, request ReplyRequest) (ProviderMessageID, error)
}
