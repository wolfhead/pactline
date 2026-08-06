package notification

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

type RecipientResolver interface {
	GetExternalIdentityForUser(context.Context, uuid.UUID) (identity.ExternalIdentity, error)
}

type AccessHandler struct {
	Recipients RecipientResolver
	Sender     Sender
	AppBaseURL *url.URL
}

func (handler AccessHandler) Handle(ctx context.Context, event events.Event) error {
	if handler.Recipients == nil || handler.Sender == nil || handler.AppBaseURL == nil {
		return fmt.Errorf("access notification handler is not configured")
	}
	card, err := handler.card(event)
	if err != nil {
		return err
	}
	recipient, err := handler.Recipients.GetExternalIdentityForUser(ctx, event.RecipientID)
	if err != nil {
		return fmt.Errorf("resolve notification recipient: %w", err)
	}
	if recipient.Key.Provider != "lark" {
		return fmt.Errorf("unsupported notification recipient provider")
	}
	if _, err := handler.Sender.SendCard(ctx, recipient.Key, card, event.ID.String()); err != nil {
		return fmt.Errorf("send access notification: %w", err)
	}
	return nil
}

func (handler AccessHandler) card(event events.Event) (Card, error) {
	if event.AggregateType != "access_request" {
		return Card{}, fmt.Errorf("invalid access notification aggregate")
	}
	switch event.Type {
	case events.AccessRequested:
		payload, err := events.DecodePayload[events.AccessRequestedPayload](event)
		if err != nil {
			return Card{}, err
		}
		if payload.RequesterID != event.AggregateID {
			return Card{}, fmt.Errorf("access request event aggregate mismatch")
		}
		email := "未提供"
		if payload.RequesterEmail != nil && strings.TrimSpace(*payload.RequesterEmail) != "" {
			email = strings.TrimSpace(*payload.RequesterEmail)
		}
		return Card{
			Title: "新的 Pactline 访问申请",
			Lines: []string{
				"申请人：" + strings.TrimSpace(payload.RequesterName),
				"邮箱：" + email,
				"申请时间：" + displayTime(payload.RequestedAt),
			},
			ActionLabel: "前往 Pactline 审批",
			ActionURL:   handler.resolvePath("/admin/users"),
			Template:    "blue",
		}, nil
	case events.AccessApproved:
		payload, err := events.DecodePayload[events.AccessApprovedPayload](event)
		if err != nil {
			return Card{}, err
		}
		if payload.UserID != event.AggregateID || payload.UserID != event.RecipientID {
			return Card{}, fmt.Errorf("access approval event recipient mismatch")
		}
		return Card{
			Title: "Pactline 访问申请已通过",
			Lines: []string{
				strings.TrimSpace(payload.ApprovedByName) + " 已通过你的访问申请。",
				"通过时间：" + displayTime(payload.ApprovedAt),
			},
			ActionLabel: "进入 Pactline",
			ActionURL:   handler.resolvePath("/"),
			Template:    "green",
		}, nil
	default:
		return Card{}, fmt.Errorf("unsupported access notification event %q", event.Type)
	}
}

func (handler AccessHandler) resolvePath(path string) string {
	return handler.AppBaseURL.ResolveReference(&url.URL{Path: path}).String()
}

func displayTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04 UTC")
}
