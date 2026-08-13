package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/notification"
)

func (c *Client) SendCard(
	ctx context.Context,
	recipient identity.PrincipalKey,
	card notification.Card,
	idempotencyKey string,
) (identity.DeliveryReceipt, error) {
	if recipient.Provider != "lark" || recipient.TenantID != c.TenantKey() ||
		strings.TrimSpace(recipient.SubjectID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return identity.DeliveryReceipt{}, providerError(
			"send_notification", identity.ProviderContract, "", errors.New("invalid notification recipient"))
	}
	if err := card.Validate(); err != nil {
		return identity.DeliveryReceipt{}, providerError("send_notification", identity.ProviderContract, "", err)
	}
	tenantToken, err := c.tenantAccessToken(ctx)
	if err != nil {
		return identity.DeliveryReceipt{}, err
	}
	content, err := json.Marshal(larkNotificationCard(card))
	if err != nil {
		return identity.DeliveryReceipt{}, providerError("send_notification", identity.ProviderContract, "", err)
	}
	var response providerEnvelope[messageData]
	requestID, err := c.doJSON(
		ctx,
		"send_notification",
		http.MethodPost,
		"/open-apis/im/v1/messages?receive_id_type=open_id",
		tenantToken,
		map[string]string{
			"receive_id": recipient.SubjectID,
			"msg_type":   "interactive",
			"content":    string(content),
			"uuid":       idempotencyKey,
		},
		&response,
	)
	if err != nil {
		return identity.DeliveryReceipt{}, err
	}
	if response.Code != 0 || strings.TrimSpace(response.Data.MessageID) == "" {
		return identity.DeliveryReceipt{}, providerError(
			"send_notification",
			classifyProviderCode(response.Code),
			firstNonEmpty(requestID, response.Error.LogID),
			fmt.Errorf("provider code %d or incomplete notification delivery", response.Code),
		)
	}
	return identity.DeliveryReceipt{
		ProviderReference: response.Data.MessageID,
		RequestID:         requestID,
	}, nil
}

func larkNotificationCard(card notification.Card) map[string]any {
	elements := make([]map[string]any, 0, len(card.Lines)+1)
	for _, line := range card.Lines {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]string{"tag": "plain_text", "content": line},
		})
	}
	elements = append(elements, map[string]any{
		"tag": "action",
		"actions": []map[string]any{{
			"tag":  "button",
			"type": "primary",
			"text": map[string]string{"tag": "plain_text", "content": card.ActionLabel},
			"url":  card.ActionURL,
		}},
	})
	return map[string]any{
		"config": map[string]bool{"wide_screen_mode": true},
		"header": map[string]any{
			"template": card.Template,
			"title":    map[string]string{"tag": "plain_text", "content": card.Title},
		},
		"elements": elements,
	}
}
