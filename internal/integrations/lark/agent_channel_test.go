package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/identity"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"
)

func TestAgentChannelDiscoversBotAndNormalizesExplicitMention(t *testing.T) {
	server := newAgentChannelInitializationServer(t)
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())
	payload := []byte(`{
		"schema":"2.0",
		"header":{
			"event_id":"event-1",
			"event_type":"im.message.receive_v1",
			"app_id":"app-id",
			"tenant_key":"tenant"
		},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"},"tenant_key":"tenant"},
			"message":{
				"message_id":"message-1",
				"root_id":"root-1",
				"parent_id":"parent-1",
				"create_time":"1785398400000",
				"chat_id":"chat-1",
				"message_type":"text",
				"content":"{\"text\":\"@_user_1 帮我创建一个任务\"}",
				"mentions":[{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"Pactline"}]
			}
		}
	}`)
	var event larkim.P2MessageReceiveV1
	require.NoError(t, json.Unmarshal(payload, &event))
	incoming, err := client.NormalizeMessageEvent(&event)
	require.NoError(t, err)
	require.True(t, incoming.BotMentioned)
	require.Equal(t, "帮我创建一个任务", incoming.Text)
	require.Equal(t, "ou_user", incoming.SenderSubjectID)
	require.Equal(t, "parent-1", incoming.ReplyParentMessageID)
}

func TestAgentChannelFetchesBoundedContextAndReplies(t *testing.T) {
	trigger := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	var replyBody map[string]string
	var reactionBody map[string]map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)
		case r.URL.Path == "/open-apis/tenant/v2/tenant/query":
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"code":0,"data":{"tenant":{"tenant_key":"tenant"}}}`)
		case r.URL.Path == "/open-apis/bot/v3/info":
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","bot":{"activate_status":2,"open_id":"ou_bot"}}`)
		case r.URL.Path == "/open-apis/im/v1/messages" && r.Method == http.MethodGet:
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			require.Equal(t, "chat-1", r.URL.Query().Get("container_id"))
			require.Equal(t, "2", r.URL.Query().Get("page_size"))
			_, _ = io.WriteString(w, `{
				"code":0,
				"data":{
					"has_more":true,
					"page_token":"next-page",
					"items":[
						{"message_id":"m1","chat_id":"chat-1","message_type":"text","create_time":"1785398340000","content":"{\"text\":\"first\"}"},
						{"message_id":"m2","chat_id":"chat-1","message_type":"text","create_time":"1785398280000","content":"{\"text\":\"second\"}"}
					]
				}
			}`)
		case r.URL.Path == "/open-apis/im/v1/messages/message-1/reply":
			require.Equal(t, http.MethodPost, r.Method)
			require.NotEmpty(t, r.URL.Query().Get("uuid"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&replyBody))
			_, _ = io.WriteString(w, `{"code":0,"data":{"message_id":"reply-1"}}`)
		case r.URL.Path == "/open-apis/im/v1/messages/message-1/reactions":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&reactionBody))
			_, _ = io.WriteString(w, `{
				"code":0,
				"data":{"reaction_id":"reaction-1"}
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())

	messages, err := client.FetchContext(context.Background(), channel.ContextRequest{
		TenantID: "tenant", ConversationID: "chat-1", TriggerMessageID: "message-1",
		PageSize: 2, NotBefore: trigger.Add(-time.Hour), NotAfter: trigger,
	})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "first", messages[0].Text)
	require.Equal(t, "next-page", messages[1].Cursor)

	providerID, err := client.Reply(context.Background(), channel.ReplyRequest{
		TenantID: "tenant", ConversationID: "chat-1", TargetMessageID: "message-1",
		Body: "created", IdempotencyKey: "run:success",
	})
	require.NoError(t, err)
	require.Equal(t, channel.ProviderMessageID("reply-1"), providerID)
	require.Equal(t, "text", replyBody["msg_type"])
	require.JSONEq(t, `{"text":"created"}`, replyBody["content"])

	require.NoError(t, client.Acknowledge(context.Background(), channel.AcknowledgeRequest{
		TenantID:        "tenant",
		TargetMessageID: "message-1",
	}))
	require.Equal(t, "OnIt", reactionBody["reaction_type"]["emoji_type"])
}

func newAgentChannelTestClient(
	t *testing.T,
	baseURL string,
	httpClient *http.Client,
) *Client {
	t.Helper()
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	client, err := NewClient(Config{
		AppID: "app-id", AppSecret: "app-secret",
		BaseURL: baseURL, RedirectURI: "https://tasks.example.test/callback",
		Cipher: cipher, EncryptionKeyID: "key-1", HTTPClient: httpClient,
	})
	require.NoError(t, err)
	tenantKey, err := client.InitializeTenant(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tenant", tenantKey)
	require.NoError(t, client.InitializeAgentChannel(context.Background()))
	return client
}

func newAgentChannelInitializationServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)
		case "/open-apis/tenant/v2/tenant/query":
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"code":0,"data":{"tenant":{"tenant_key":"tenant"}}}`)
		case "/open-apis/bot/v3/info":
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","bot":{"activate_status":2,"open_id":"ou_bot"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}
