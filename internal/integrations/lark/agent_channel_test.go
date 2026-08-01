package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
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

func TestAgentChannelNormalizesLarkPostAsPlainText(t *testing.T) {
	server := newAgentChannelInitializationServer(t)
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())
	payload := []byte(`{
		"schema":"2.0",
		"header":{
			"event_id":"event-post",
			"event_type":"im.message.receive_v1",
			"app_id":"app-id",
			"tenant_key":"tenant"
		},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"},"tenant_key":"tenant"},
			"message":{
				"message_id":"message-post",
				"create_time":"1785420486741",
				"chat_id":"chat-1",
				"message_type":"post",
				"content":"{\"title\":\"\",\"content\":[[{\"tag\":\"at\",\"user_id\":\"@_user_1\",\"user_name\":\"Pactline\",\"style\":[]},{\"tag\":\"text\",\"text\":\"  \",\"style\":[]},{\"tag\":\"text\",\"text\":\"查看 Task #1 的状态\",\"style\":[]},{\"tag\":\"img\",\"image_key\":\"post-image-key\"}]],\"content_v2\":[[{\"tag\":\"at\",\"user_id\":\"@_user_1\",\"user_name\":\"Pactline\",\"style\":[]},{\"tag\":\"text\",\"text\":\"  \",\"style\":[]},{\"tag\":\"text\",\"text\":\"查看 Task #1 的状态\",\"style\":[]},{\"tag\":\"img\",\"image_key\":\"post-image-key\"}]]}",
				"mentions":[{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"Pactline"}]
			}
		}
	}`)
	var event larkim.P2MessageReceiveV1
	require.NoError(t, json.Unmarshal(payload, &event))

	incoming, err := client.NormalizeMessageEvent(&event)

	require.NoError(t, err)
	require.True(t, incoming.BotMentioned)
	require.Equal(t, "post", incoming.MessageType)
	require.Equal(t, "查看 Task #1 的状态", incoming.Text)
	require.Len(t, incoming.Artifacts, 1)
	require.Equal(t, "image", string(incoming.Artifacts[0].Kind))
}

func TestAgentChannelNormalizesImageAsArtifact(t *testing.T) {
	server := newAgentChannelInitializationServer(t)
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())
	payload := []byte(`{
		"schema":"2.0",
		"header":{
			"event_id":"event-image",
			"event_type":"im.message.receive_v1",
			"app_id":"app-id",
			"tenant_key":"tenant"
		},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"},"tenant_key":"tenant"},
			"message":{
				"message_id":"message-image",
				"create_time":"1785420486741",
				"chat_id":"chat-1",
				"message_type":"image",
				"content":"{\"image_key\":\"img-key\"}"
			}
		}
	}`)
	var event larkim.P2MessageReceiveV1
	require.NoError(t, json.Unmarshal(payload, &event))

	incoming, err := client.NormalizeMessageEvent(&event)

	require.NoError(t, err)
	require.Empty(t, incoming.Text)
	require.False(t, incoming.BotMentioned)
	require.Len(t, incoming.Artifacts, 1)
	require.Equal(t, "image", string(incoming.Artifacts[0].Kind))
	require.NotEmpty(t, incoming.Artifacts[0].ID)
}

func TestExtractMessageContentRejectsStickerWithoutRegisteringArtifact(t *testing.T) {
	text, artifacts, err := extractMessageContent("sticker", `{"file_key":"sticker-key"}`)

	require.ErrorIs(t, err, channel.ErrUnsupportedMessage)
	require.Empty(t, text)
	require.Empty(t, artifacts)
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
						{
							"message_id":"m1",
							"chat_id":"chat-1",
							"msg_type":"text",
							"create_time":"1785398340000",
							"sender":{"id":"ou_first"},
							"body":{"content":"{\"text\":\"first\"}"}
						},
						{
							"message_id":"m2",
							"chat_id":"chat-1",
							"msg_type":"post",
							"create_time":"1785398280000",
							"sender":{"id":"ou_second"},
							"body":{"content":"{\"title\":\"\",\"content\":[[{\"tag\":\"text\",\"text\":\"second\"}]]}"}
						}
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
	require.Equal(t, "ou_first", messages[0].SenderSubjectID)
	require.Equal(t, "second", messages[1].Text)
	require.Equal(t, "ou_second", messages[1].SenderSubjectID)
	require.Equal(t, "next-page", messages[1].Cursor)

	providerID, err := client.Reply(context.Background(), channel.ReplyRequest{
		TenantID: "tenant", ConversationID: "chat-1", TargetMessageID: "message-1",
		Body: "created", IdempotencyKey: "run:success",
	})
	require.NoError(t, err)
	require.Equal(t, channel.ProviderMessageID("reply-1"), providerID)
	require.Equal(t, "interactive", replyBody["msg_type"])
	var card struct {
		Header struct {
			Template string `json:"template"`
			Title    struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
		Elements []struct {
			Tag  string `json:"tag"`
			Text struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"text"`
		} `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(replyBody["content"]), &card))
	require.Equal(t, "blue", card.Header.Template)
	require.Equal(t, "Pactline Agent", card.Header.Title.Content)
	require.Len(t, card.Elements, 1)
	require.Equal(t, "lark_md", card.Elements[0].Text.Tag)
	require.Equal(t, "created", card.Elements[0].Text.Content)

	require.NoError(t, client.Acknowledge(context.Background(), channel.AcknowledgeRequest{
		TenantID:        "tenant",
		TargetMessageID: "message-1",
	}))
	require.Equal(t, "OnIt", reactionBody["reaction_type"]["emoji_type"])
}

func TestAgentArtifactResolverDownloadsOnlyRegisteredRunScope(t *testing.T) {
	createdAt := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)
		case "/open-apis/tenant/v2/tenant/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"data":{"tenant":{"tenant_key":"tenant"}}}`)
		case "/open-apis/bot/v3/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok","bot":{"activate_status":2,"open_id":"ou_bot"}}`)
		case "/open-apis/im/v1/messages/message-1/resources/file-key":
			require.Equal(t, "file", r.URL.Query().Get("type"))
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "text/csv")
			_, _ = io.WriteString(w, "id,rate\na,0.2\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())
	references := client.registerArtifacts("tenant", "chat-1", "message-1", createdAt, []rawMessageArtifact{{
		ProviderKey: "file-key", ResourceType: "file", Kind: artifact.KindCSV,
		Name: "accounts.csv", MediaType: "text/csv",
	}})
	require.Len(t, references, 1)
	scope := artifact.Scope{
		RunID: uuid.New(), TenantID: "tenant", ConversationID: "chat-1",
		NotBefore: createdAt.Add(-time.Hour), NotAfter: createdAt,
	}

	local, err := client.Resolve(context.Background(), scope, references[0].ID)
	require.NoError(t, err)
	encoded, err := os.ReadFile(local.Path)
	require.NoError(t, err)
	require.Equal(t, "id,rate\na,0.2\n", string(encoded))
	require.NoError(t, local.Cleanup())
	_, err = os.Stat(local.Path)
	require.ErrorIs(t, err, os.ErrNotExist)

	scope.ConversationID = "other-chat"
	_, err = client.Resolve(context.Background(), scope, references[0].ID)
	require.ErrorIs(t, err, artifact.ErrScope)
}

func TestBuildAgentMarkdownCardPromotesFirstHeading(t *testing.T) {
	encoded, err := buildAgentMarkdownCard(
		"# 📋 Task #42 · Inspect\\_status\n\n**状态**：in\\_progress\n\n[打开](https://tasks.example.test/tasks/42)",
	)
	require.NoError(t, err)

	var card struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
		Elements []struct {
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"elements"`
	}
	require.NoError(t, json.Unmarshal(encoded, &card))
	require.Equal(t, "📋 Task #42 · Inspect_status", card.Header.Title.Content)
	require.Equal(
		t,
		"**状态**：in\\_progress\n\n[打开](https://tasks.example.test/tasks/42)",
		card.Elements[0].Text.Content,
	)
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
