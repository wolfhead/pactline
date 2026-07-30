package lark

import (
	"context"
	"crypto/sha256"
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

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var ErrAgentChannelNotConfigured = errors.New("Lark Agent channel is not configured")

func (c *Client) AgentChannelReady() bool {
	if c == nil {
		return false
	}
	c.botMu.RLock()
	defer c.botMu.RUnlock()
	return c.botOpenID != ""
}

func (c *Client) InitializeAgentChannel(ctx context.Context) error {
	if c == nil {
		return ErrAgentChannelNotConfigured
	}
	tenantToken, err := c.tenantAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("initialize Lark Agent channel: %w", err)
	}
	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			ActivateStatus int    `json:"activate_status"`
			OpenID         string `json:"open_id"`
		} `json:"bot"`
	}
	requestID, err := c.doJSON(
		ctx,
		"get_bot_info",
		http.MethodGet,
		"/open-apis/bot/v3/info",
		tenantToken,
		nil,
		&response,
	)
	if err != nil {
		return fmt.Errorf("initialize Lark Agent channel: %w", err)
	}
	if response.Code != 0 || strings.TrimSpace(response.Bot.OpenID) == "" {
		return providerError(
			"get_bot_info",
			classifyProviderCode(response.Code),
			requestID,
			fmt.Errorf("provider code %d or incomplete bot identity", response.Code),
		)
	}
	if response.Bot.ActivateStatus != 2 {
		return providerError(
			"get_bot_info",
			identity.ProviderContract,
			requestID,
			fmt.Errorf("bot is not activated (status %d)", response.Bot.ActivateStatus),
		)
	}
	c.botMu.Lock()
	c.botOpenID = strings.TrimSpace(response.Bot.OpenID)
	c.botMu.Unlock()
	return nil
}

func (c *Client) NormalizeMessageEvent(
	event *larkim.P2MessageReceiveV1,
) (channel.IncomingMessage, error) {
	if !c.AgentChannelReady() {
		return channel.IncomingMessage{}, ErrAgentChannelNotConfigured
	}
	if event == nil || event.EventV2Base == nil ||
		event.EventV2Base.Header == nil || event.Event == nil ||
		event.Event.Sender == nil || event.Event.Message == nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	header := event.EventV2Base.Header
	message := event.Event.Message
	sender := event.Event.Sender
	if event.Schema != "2.0" ||
		header.EventType != "im.message.receive_v1" ||
		header.EventID == "" ||
		header.AppID != c.appID ||
		header.TenantKey != c.tenantKey ||
		sender.SenderId == nil ||
		stringValue(sender.SenderId.OpenId) == "" ||
		stringValue(sender.TenantKey) != c.tenantKey ||
		stringValue(message.MessageId) == "" ||
		stringValue(message.ChatId) == "" {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	if stringValue(message.MessageType) != "text" {
		return channel.IncomingMessage{}, channel.ErrUnsupportedMessage
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(stringValue(message.Content)), &content); err != nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	c.botMu.RLock()
	botOpenID := c.botOpenID
	c.botMu.RUnlock()
	var mentions []channel.Mention
	botMentioned := false
	text := content.Text
	for _, mention := range message.Mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		subjectID := stringValue(mention.Id.OpenId)
		isBot := subjectID == botOpenID
		if isBot {
			botMentioned = true
			text = strings.ReplaceAll(text, stringValue(mention.Key), "")
		}
		mentions = append(mentions, channel.Mention{
			SubjectID: subjectID,
			Name:      stringValue(mention.Name),
			IsBot:     isBot,
		})
	}
	createdAt, err := providerMillis(stringValue(message.CreateTime))
	if err != nil {
		return channel.IncomingMessage{}, channel.ErrInvalidEvent
	}
	return channel.IncomingMessage{
		Provider:             "lark",
		TenantID:             header.TenantKey,
		EventID:              header.EventID,
		ConversationID:       stringValue(message.ChatId),
		MessageID:            stringValue(message.MessageId),
		ThreadRootMessageID:  stringValue(message.RootId),
		ReplyParentMessageID: stringValue(message.ParentId),
		SenderSubjectID:      stringValue(sender.SenderId.OpenId),
		MessageType:          stringValue(message.MessageType),
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

type larkMessage struct {
	MessageID   string `json:"message_id"`
	RootID      string `json:"root_id"`
	ParentID    string `json:"parent_id"`
	CreateTime  string `json:"create_time"`
	ChatID      string `json:"chat_id"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
	SenderID    string `json:"sender_id"`
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ channel.ChannelAdapter = (*Client)(nil)
var _ identity.NotificationSender = (*Client)(nil)
