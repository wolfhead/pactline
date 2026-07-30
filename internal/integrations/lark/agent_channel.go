package lark

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/identity"
)

var ErrAgentChannelNotConfigured = errors.New("Lark Agent channel is not configured")

func (c *Client) AgentChannelReady() bool {
	return c != nil &&
		c.eventVerificationToken != "" &&
		c.botOpenID != ""
}

func (c *Client) VerifyCallback(headers http.Header, payload []byte) error {
	if !c.AgentChannelReady() {
		return ErrAgentChannelNotConfigured
	}
	if len(payload) == 0 || len(payload) > 2<<20 {
		return channel.ErrInvalidEvent
	}
	if c.eventEncryptKey == "" {
		return nil
	}
	timestamp := strings.TrimSpace(headers.Get("X-Lark-Request-Timestamp"))
	nonce := strings.TrimSpace(headers.Get("X-Lark-Request-Nonce"))
	signature := strings.TrimSpace(headers.Get("X-Lark-Signature"))
	if timestamp == "" || nonce == "" || signature == "" {
		return channel.ErrInvalidEvent
	}
	digest := sha256.Sum256(append(
		[]byte(timestamp+nonce+c.eventEncryptKey),
		payload...,
	))
	expected := hex.EncodeToString(digest[:])
	if len(expected) != len(signature) ||
		subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) != 1 {
		return channel.ErrInvalidEvent
	}
	return nil
}

func (c *Client) CallbackChallenge(payload []byte) (string, bool, error) {
	var request struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return "", false, channel.ErrInvalidEvent
	}
	if request.Type != "url_verification" {
		return "", false, nil
	}
	if request.Token != c.eventVerificationToken || request.Challenge == "" {
		return "", true, channel.ErrInvalidEvent
	}
	return request.Challenge, true, nil
}

func (c *Client) NormalizeEvent(
	_ context.Context,
	payload []byte,
) (channel.IncomingMessage, error) {
	if !c.AgentChannelReady() {
		return channel.IncomingMessage{}, ErrAgentChannelNotConfigured
	}
	var envelope messageEventEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	if envelope.Encrypt != "" {
		return channel.IncomingMessage{}, fmt.Errorf(
			"%w: encrypted callbacks are unsupported; callback signature verification is required",
			channel.ErrInvalidEvent,
		)
	}
	if envelope.Schema != "2.0" ||
		envelope.Header.EventType != "im.message.receive_v1" ||
		envelope.Header.EventID == "" ||
		envelope.Header.Token != c.eventVerificationToken ||
		envelope.Header.AppID != c.appID ||
		envelope.Header.TenantKey != c.tenantKey {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	message := envelope.Event.Message
	sender := envelope.Event.Sender
	if message.MessageID == "" || message.ChatID == "" ||
		sender.SenderID.OpenID == "" ||
		sender.TenantKey != c.tenantKey {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	if message.MessageType != "text" {
		return channel.IncomingMessage{}, channel.ErrUnsupportedMessage
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(message.Content), &content); err != nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	var mentions []channel.Mention
	botMentioned := false
	text := content.Text
	for _, mention := range message.Mentions {
		isBot := mention.ID.OpenID == c.botOpenID
		if isBot {
			botMentioned = true
			text = strings.ReplaceAll(text, mention.Key, "")
		}
		mentions = append(mentions, channel.Mention{
			SubjectID: mention.ID.OpenID,
			Name:      mention.Name,
			IsBot:     isBot,
		})
	}
	createdAt, err := providerMillis(message.CreateTime)
	if err != nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	return channel.IncomingMessage{
		Provider:             "lark",
		TenantID:             envelope.Header.TenantKey,
		EventID:              envelope.Header.EventID,
		ConversationID:       message.ChatID,
		MessageID:            message.MessageID,
		ThreadRootMessageID:  message.RootID,
		ReplyParentMessageID: message.ParentID,
		SenderSubjectID:      sender.SenderID.OpenID,
		MessageType:          message.MessageType,
		Text:                 strings.TrimSpace(text),
		Mentions:             mentions,
		CreatedAt:            createdAt,
		BotMentioned:         botMentioned,
	}, nil
}

func (c *Client) FetchContext(
	ctx context.Context,
	request channel.ContextRequest,
) ([]channel.ChannelMessage, error) {
	if !c.AgentChannelReady() || request.TenantID != c.tenantKey {
		return nil, channel.ErrContextBoundary
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	tenantToken, err := c.tenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	parameters := url.Values{}
	parameters.Set("container_id_type", "chat")
	parameters.Set("container_id", request.ConversationID)
	parameters.Set("start_time", strconv.FormatInt(request.NotBefore.Unix(), 10))
	parameters.Set("end_time", strconv.FormatInt(request.NotAfter.Unix(), 10))
	parameters.Set("sort_type", "ByCreateTimeDesc")
	parameters.Set("page_size", strconv.Itoa(request.PageSize))
	if request.BeforeCursor != "" {
		parameters.Set("page_token", request.BeforeCursor)
	}
	var response providerEnvelope[messageListData]
	requestID, err := c.doJSON(
		ctx,
		"fetch_agent_context",
		http.MethodGet,
		"/open-apis/im/v1/messages?"+parameters.Encode(),
		tenantToken,
		nil,
		&response,
	)
	if err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, providerError(
			"fetch_agent_context",
			classifyProviderCode(response.Code),
			firstNonEmpty(requestID, response.Error.LogID),
			fmt.Errorf("provider code %d", response.Code),
		)
	}
	messages := make([]channel.ChannelMessage, 0, len(response.Data.Items))
	for _, item := range response.Data.Items {
		if item.ChatID != request.ConversationID || item.MessageType != "text" {
			continue
		}
		createdAt, parseErr := providerMillis(item.CreateTime)
		if parseErr != nil ||
			createdAt.Before(request.NotBefore) ||
			!createdAt.Before(request.NotAfter) {
			continue
		}
		var content struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(item.Content), &content) != nil {
			continue
		}
		messages = append(messages, channel.ChannelMessage{
			MessageID:       item.MessageID,
			SenderSubjectID: item.SenderID,
			Text:            strings.TrimSpace(content.Text),
			CreatedAt:       createdAt,
		})
		if len(messages) == request.PageSize {
			break
		}
	}
	if len(messages) > 0 && response.Data.HasMore {
		messages[len(messages)-1].Cursor = response.Data.PageToken
	}
	return messages, nil
}

func (c *Client) Reply(
	ctx context.Context,
	request channel.ReplyRequest,
) (channel.ProviderMessageID, error) {
	if !c.AgentChannelReady() ||
		request.TenantID != c.tenantKey ||
		request.ConversationID == "" ||
		request.TargetMessageID == "" ||
		strings.TrimSpace(request.Body) == "" ||
		request.IdempotencyKey == "" {
		return "", channel.ErrInvalidEvent
	}
	tenantToken, err := c.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(map[string]string{"text": request.Body})
	idempotencyHash := sha256.Sum256([]byte(request.IdempotencyKey))
	path := "/open-apis/im/v1/messages/" +
		url.PathEscape(request.TargetMessageID) +
		"/reply?uuid=" + hex.EncodeToString(idempotencyHash[:16])
	var response providerEnvelope[messageData]
	requestID, err := c.doJSON(
		ctx,
		"reply_agent_message",
		http.MethodPost,
		path,
		tenantToken,
		map[string]string{"msg_type": "text", "content": string(content)},
		&response,
	)
	if err != nil {
		return "", err
	}
	if response.Code != 0 || response.Data.MessageID == "" {
		return "", providerError(
			"reply_agent_message",
			classifyProviderCode(response.Code),
			firstNonEmpty(requestID, response.Error.LogID),
			fmt.Errorf("provider code %d", response.Code),
		)
	}
	return channel.ProviderMessageID(response.Data.MessageID), nil
}

type messageEventEnvelope struct {
	Schema  string `json:"schema"`
	Encrypt string `json:"encrypt"`
	Header  struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		AppID     string `json:"app_id"`
		TenantKey string `json:"tenant_key"`
		Token     string `json:"token"`
	} `json:"header"`
	Event struct {
		Sender  larkMessageSender `json:"sender"`
		Message larkMessage       `json:"message"`
	} `json:"event"`
}

type larkMessageSender struct {
	SenderID struct {
		OpenID string `json:"open_id"`
	} `json:"sender_id"`
	TenantKey string `json:"tenant_key"`
}

type larkMention struct {
	Key string `json:"key"`
	ID  struct {
		OpenID string `json:"open_id"`
	} `json:"id"`
	Name string `json:"name"`
}

type larkMessage struct {
	MessageID   string        `json:"message_id"`
	RootID      string        `json:"root_id"`
	ParentID    string        `json:"parent_id"`
	CreateTime  string        `json:"create_time"`
	ChatID      string        `json:"chat_id"`
	MessageType string        `json:"message_type"`
	Content     string        `json:"content"`
	Mentions    []larkMention `json:"mentions"`
	SenderID    string        `json:"sender_id"`
}

type messageListData struct {
	Items     []larkMessage `json:"items"`
	HasMore   bool          `json:"has_more"`
	PageToken string        `json:"page_token"`
}

func providerMillis(value string) (time.Time, error) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, channel.ErrInvalidEvent
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

var _ channel.ChannelAdapter = (*Client)(nil)
var _ identity.NotificationSender = (*Client)(nil)
