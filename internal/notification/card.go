package notification

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/wolfhead/pactline/internal/identity"
)

type Card struct {
	Title       string
	Lines       []string
	ActionLabel string
	ActionURL   string
	Template    string
}

func (card Card) Validate() error {
	if strings.TrimSpace(card.Title) == "" || len(card.Lines) == 0 ||
		strings.TrimSpace(card.ActionLabel) == "" || strings.TrimSpace(card.ActionURL) == "" {
		return fmt.Errorf("invalid notification card")
	}
	if len([]rune(card.Title)) > 80 || len(card.Lines) > 10 || len([]rune(card.ActionLabel)) > 40 {
		return fmt.Errorf("notification card exceeds presentation limits")
	}
	parsed, err := url.ParseRequestURI(card.ActionURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid notification action URL")
	}
	for _, line := range card.Lines {
		if strings.TrimSpace(line) == "" || len([]rune(line)) > 500 {
			return fmt.Errorf("invalid notification card line")
		}
	}
	return nil
}

type Sender interface {
	SendCard(
		ctx context.Context,
		recipient identity.PrincipalKey,
		card Card,
		idempotencyKey string,
	) (identity.DeliveryReceipt, error)
}
