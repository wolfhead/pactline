package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/notification"

	"github.com/stretchr/testify/require"
)

func TestNotificationChannelSendsIdempotentInteractiveCard(t *testing.T) {
	var message map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)
		case "/open-apis/tenant/v2/tenant/query":
			_, _ = io.WriteString(w, `{"code":0,"data":{"tenant":{"tenant_key":"tenant"}}}`)
		case "/open-apis/bot/v3/info":
			_, _ = io.WriteString(w, `{"code":0,"bot":{"activate_status":2,"open_id":"ou_bot"}}`)
		case "/open-apis/im/v1/messages":
			require.Equal(t, "open_id", r.URL.Query().Get("receive_id_type"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&message))
			_, _ = io.WriteString(w, `{"code":0,"data":{"message_id":"message-notification"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())

	receipt, err := client.SendCard(
		context.Background(),
		identity.PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_recipient"},
		notification.Card{
			Title: "Access requested", Lines: []string{"Applicant: User"},
			ActionLabel: "Review", ActionURL: "https://pactline.example.test/admin/users",
			Template: "blue",
		},
		"event-idempotency-key",
	)
	require.NoError(t, err)
	require.Equal(t, "message-notification", receipt.ProviderReference)
	require.Equal(t, "ou_recipient", message["receive_id"])
	require.Equal(t, "interactive", message["msg_type"])
	require.Equal(t, "event-idempotency-key", message["uuid"])
	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(message["content"]), &card))
	require.NotEmpty(t, card["header"])
	require.NotEmpty(t, card["elements"])
}
