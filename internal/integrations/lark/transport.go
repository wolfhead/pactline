package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/larkaudit"

	"github.com/google/uuid"
)

const (
	maxJSONResponseBytes = 2 << 20
	auditWriteTimeout    = 2 * time.Second
)

var errResponseTooLarge = errors.New("Lark response exceeds configured limit")

type JSONCall struct {
	Descriptor larkaudit.Call
	Path       string
	Token      string
	Input      any
	Output     any
}

type DownloadCall struct {
	Descriptor larkaudit.Call
	Path       string
	Token      string
	Target     io.Writer
	MaxBytes   int64
}

type CallResult struct {
	HTTPStatus        int
	ProviderCode      *int
	ProviderRequestID string
	RequestBytes      int64
	ResponseBytes     int64
	ContentType       string
}

type Transport interface {
	DoJSON(context.Context, JSONCall) (CallResult, error)
	Download(context.Context, DownloadCall) (CallResult, error)
}

type httpTransport struct {
	baseURL string
	client  *http.Client
}

func (transport httpTransport) DoJSON(
	ctx context.Context,
	call JSONCall,
) (CallResult, error) {
	if err := call.Descriptor.Validate(); err != nil {
		return CallResult{}, providerError(
			call.Descriptor.Operation, identity.ProviderContract, "", err)
	}
	var body io.Reader
	var encoded []byte
	if call.Input != nil {
		var err error
		encoded, err = json.Marshal(call.Input)
		if err != nil {
			return CallResult{}, providerError(
				call.Descriptor.Operation, identity.ProviderContract, "", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(
		ctx, call.Descriptor.Method, transport.baseURL+call.Path, body,
	)
	if err != nil {
		return CallResult{RequestBytes: int64(len(encoded))}, providerError(
			call.Descriptor.Operation, identity.ProviderContract, "", err)
	}
	request.Header.Set("Accept", "application/json")
	if call.Input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if call.Token != "" {
		request.Header.Set("Authorization", "Bearer "+call.Token)
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return CallResult{RequestBytes: int64(len(encoded))}, providerError(
			call.Descriptor.Operation, identity.ProviderUnavailable, "", err)
	}
	defer response.Body.Close()
	result := CallResult{
		HTTPStatus: response.StatusCode, RequestBytes: int64(len(encoded)),
		ProviderRequestID: response.Header.Get("X-Tt-Logid"),
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, maxJSONResponseBytes+1))
	result.ResponseBytes = int64(len(bodyBytes))
	if err != nil {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderUnavailable,
			result.ProviderRequestID, err)
	}
	if len(bodyBytes) > maxJSONResponseBytes {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderContract,
			result.ProviderRequestID, errResponseTooLarge)
	}
	var metadata struct {
		Code  *int `json:"code"`
		Error struct {
			LogID string `json:"log_id"`
		} `json:"error"`
	}
	if json.Unmarshal(bodyBytes, &metadata) == nil {
		result.ProviderCode = metadata.Code
		result.ProviderRequestID = firstNonEmpty(
			result.ProviderRequestID, metadata.Error.LogID)
	}
	if err := validateHTTPResponse(call.Descriptor.Operation, result); err != nil {
		return result, err
	}
	if err := json.Unmarshal(bodyBytes, call.Output); err != nil {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderContract,
			result.ProviderRequestID, err)
	}
	return result, nil
}

func (transport httpTransport) Download(
	ctx context.Context,
	call DownloadCall,
) (CallResult, error) {
	if err := call.Descriptor.Validate(); err != nil {
		return CallResult{}, providerError(
			call.Descriptor.Operation, identity.ProviderContract, "", err)
	}
	request, err := http.NewRequestWithContext(
		ctx, call.Descriptor.Method, transport.baseURL+call.Path, nil,
	)
	if err != nil {
		return CallResult{}, providerError(
			call.Descriptor.Operation, identity.ProviderContract, "", err)
	}
	if call.Token != "" {
		request.Header.Set("Authorization", "Bearer "+call.Token)
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return CallResult{}, providerError(
			call.Descriptor.Operation, identity.ProviderUnavailable, "", err)
	}
	defer response.Body.Close()
	result := CallResult{
		HTTPStatus:        response.StatusCode,
		ProviderRequestID: response.Header.Get("X-Tt-Logid"),
		ContentType:       response.Header.Get("Content-Type"),
	}
	if err := validateHTTPResponse(call.Descriptor.Operation, result); err != nil {
		return result, err
	}
	if call.Target == nil || call.MaxBytes <= 0 {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderContract,
			result.ProviderRequestID, errors.New("invalid download target"))
	}
	written, err := io.Copy(call.Target, io.LimitReader(response.Body, call.MaxBytes+1))
	result.ResponseBytes = written
	if err != nil {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderUnavailable,
			result.ProviderRequestID, err)
	}
	if written > call.MaxBytes {
		return result, providerError(
			call.Descriptor.Operation, identity.ProviderContract,
			result.ProviderRequestID, errResponseTooLarge)
	}
	return result, nil
}

func validateHTTPResponse(operation string, result CallResult) error {
	switch {
	case result.HTTPStatus == http.StatusTooManyRequests:
		return providerError(operation, identity.ProviderRateLimited,
			result.ProviderRequestID, errors.New("provider rate limited"))
	case result.HTTPStatus >= 500:
		return providerError(operation, identity.ProviderUnavailable,
			result.ProviderRequestID, errors.New("provider unavailable"))
	case result.HTTPStatus < 200 || result.HTTPStatus >= 300:
		return providerError(operation, identity.ProviderUnauthorized,
			result.ProviderRequestID, fmt.Errorf("provider HTTP status %d", result.HTTPStatus))
	default:
		return nil
	}
}

type auditedTransport struct {
	next   Transport
	writer larkaudit.Writer
	now    func() time.Time
}

func newAuditedTransport(next Transport, writer larkaudit.Writer) Transport {
	return &auditedTransport{next: next, writer: writer, now: func() time.Time {
		return time.Now().UTC()
	}}
}

func (transport *auditedTransport) DoJSON(
	ctx context.Context,
	call JSONCall,
) (CallResult, error) {
	started := transport.now()
	result, err := transport.next.DoJSON(ctx, call)
	transport.record(ctx, started, call.Descriptor, result, err)
	return result, err
}

func (transport *auditedTransport) Download(
	ctx context.Context,
	call DownloadCall,
) (CallResult, error) {
	started := transport.now()
	result, err := transport.next.Download(ctx, call)
	transport.record(ctx, started, call.Descriptor, result, err)
	return result, err
}

func (transport *auditedTransport) record(
	ctx context.Context,
	started time.Time,
	call larkaudit.Call,
	result CallResult,
	callErr error,
) {
	completed := transport.now()
	correlation := larkaudit.CorrelationFromContext(ctx)
	if current, ok := identity.FromContext(ctx); ok {
		actorID, subjectID := current.Actor.ID, current.Subject.ID
		correlation.ActorUserID = &actorID
		correlation.SubjectUserID = &subjectID
		if current.AgentRunID != nil {
			correlation.AgentRunID = current.AgentRunID
		}
	}
	event := larkaudit.Event{
		ID: uuid.New(), OccurredAt: started, Operation: call.Operation,
		Category: call.Category, Method: call.Method, RoutePattern: call.RoutePattern,
		CredentialKind: string(call.CredentialKind),
		Outcome:        classifyAuditOutcome(result, callErr),
		ProviderCode:   result.ProviderCode, ProviderRequestID: result.ProviderRequestID,
		DurationMS:   max(0, completed.Sub(started).Milliseconds()),
		RequestBytes: result.RequestBytes, ResponseBytes: result.ResponseBytes,
		RequestID: correlation.RequestID, ActorUserID: correlation.ActorUserID,
		SubjectUserID: correlation.SubjectUserID, AgentRunID: correlation.AgentRunID,
		ApplicationEventID: correlation.ApplicationEventID,
	}
	if result.HTTPStatus != 0 {
		event.HTTPStatus = &result.HTTPStatus
	}
	var providerErr *ProviderError
	if errors.As(callErr, &providerErr) {
		event.ErrorCategory = string(providerErr.Category)
	}
	logValues := []any{
		"operation", event.Operation, "category", event.Category,
		"method", event.Method, "route", event.RoutePattern,
		"credential_kind", event.CredentialKind, "outcome", event.Outcome,
		"duration_ms", event.DurationMS, "request_bytes", event.RequestBytes,
		"response_bytes", event.ResponseBytes,
	}
	if event.HTTPStatus != nil {
		logValues = append(logValues, "http_status", *event.HTTPStatus)
	}
	if event.ProviderCode != nil {
		logValues = append(logValues, "provider_code", *event.ProviderCode)
	}
	if event.ProviderRequestID != "" {
		logValues = append(logValues, "provider_request_id", event.ProviderRequestID)
	}
	if event.ErrorCategory != "" {
		logValues = append(logValues, "error_category", event.ErrorCategory)
	}
	if event.RequestID != "" {
		logValues = append(logValues, "request_id", event.RequestID)
	}
	if event.ActorUserID != nil {
		logValues = append(logValues, "actor_user_id", *event.ActorUserID)
	}
	if event.SubjectUserID != nil {
		logValues = append(logValues, "subject_user_id", *event.SubjectUserID)
	}
	if event.AgentRunID != nil {
		logValues = append(logValues, "agent_run_id", *event.AgentRunID)
	}
	if event.ApplicationEventID != nil {
		logValues = append(logValues, "application_event_id", *event.ApplicationEventID)
	}
	slog.Info("Lark API call completed", logValues...)
	if transport.writer == nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	if err := transport.writer.RecordLarkAPIAudit(auditCtx, event); err != nil {
		slog.Error("record Lark API audit failed",
			"operation", event.Operation,
			"provider_request_id", event.ProviderRequestID,
			"error", err)
	}
}

func classifyAuditOutcome(result CallResult, err error) larkaudit.Outcome {
	if err == nil {
		if result.ProviderCode != nil && *result.ProviderCode != 0 {
			return larkaudit.OutcomeRejected
		}
		return larkaudit.OutcomeSucceeded
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return larkaudit.OutcomeCancelled
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		return larkaudit.OutcomeContractError
	}
	switch providerErr.Category {
	case identity.ProviderRateLimited:
		return larkaudit.OutcomeRateLimited
	case identity.ProviderUnavailable:
		return larkaudit.OutcomeUnavailable
	case identity.ProviderContract:
		return larkaudit.OutcomeContractError
	default:
		return larkaudit.OutcomeRejected
	}
}

func descriptorFor(operation, method string) larkaudit.Call {
	descriptor := larkaudit.Call{
		Operation: operation, Method: method, CredentialKind: larkaudit.CredentialTenant,
	}
	switch operation {
	case "tenant_access_token":
		descriptor.Category = "identity"
		descriptor.RoutePattern = "/open-apis/auth/v3/tenant_access_token/internal"
		descriptor.CredentialKind = larkaudit.CredentialApp
	case "exchange_authorization_code", "refresh_credential":
		descriptor.Category = "identity"
		descriptor.RoutePattern = "/open-apis/authen/v2/oauth/token"
		descriptor.CredentialKind = larkaudit.CredentialApp
	case "user_info":
		descriptor.Category = "identity"
		descriptor.RoutePattern = "/open-apis/authen/v1/user_info"
		descriptor.CredentialKind = larkaudit.CredentialUser
	case "get_tenant_info":
		descriptor.Category = "identity"
		descriptor.RoutePattern = "/open-apis/tenant/v2/tenant/query"
	case "search_principals":
		descriptor.Category = "directory"
		descriptor.RoutePattern = "/open-apis/search/v1/user"
		descriptor.CredentialKind = larkaudit.CredentialUser
	case "get_principal":
		descriptor.Category = "directory"
		descriptor.RoutePattern = "/open-apis/contact/v3/users/{open_id}"
		descriptor.CredentialKind = larkaudit.CredentialUser
	case "send_invitation", "send_notification":
		descriptor.Category = "notification"
		descriptor.RoutePattern = "/open-apis/im/v1/messages"
	case "get_bot_info":
		descriptor.Category = "agent"
		descriptor.RoutePattern = "/open-apis/bot/v3/info"
	case "get_agent_conversation":
		descriptor.Category = "agent"
		descriptor.RoutePattern = "/open-apis/im/v1/chats/{chat_id}"
	case "fetch_agent_context":
		descriptor.Category = "agent"
		descriptor.RoutePattern = "/open-apis/im/v1/messages"
	case "reply_agent_message":
		descriptor.Category = "agent"
		descriptor.RoutePattern = "/open-apis/im/v1/messages/{message_id}/reply"
	case "acknowledge_agent_message":
		descriptor.Category = "agent"
		descriptor.RoutePattern = "/open-apis/im/v1/messages/{message_id}/reactions"
	case "download_agent_artifact":
		descriptor.Category = "artifact"
		descriptor.RoutePattern = "/open-apis/im/v1/messages/{message_id}/resources/{resource_key}"
	default:
		descriptor.Category = "unknown"
		descriptor.RoutePattern = "/open-apis/unknown"
	}
	return descriptor
}
