package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/larkaudit"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type transportStub struct {
	result CallResult
	err    error
}

func (stub transportStub) DoJSON(context.Context, JSONCall) (CallResult, error) {
	return stub.result, stub.err
}

func (stub transportStub) Download(context.Context, DownloadCall) (CallResult, error) {
	return stub.result, stub.err
}

type larkAuditWriterStub struct {
	event larkaudit.Event
	err   error
}

func (stub *larkAuditWriterStub) RecordLarkAPIAudit(
	_ context.Context,
	event larkaudit.Event,
) error {
	stub.event = event
	return stub.err
}

func TestAuditedTransportRecordsSafeCorrelatedMetadata(t *testing.T) {
	status, code := http.StatusOK, 0
	writer := &larkAuditWriterStub{}
	transport := newAuditedTransport(transportStub{result: CallResult{
		HTTPStatus: status, ProviderCode: &code,
		ProviderRequestID: "provider-log-1", RequestBytes: 99, ResponseBytes: 42,
	}}, writer)
	actorID, runID, eventID := uuid.New(), uuid.New(), uuid.New()
	ctx := larkaudit.WithCorrelation(context.Background(), larkaudit.Correlation{
		RequestID: "request-1", ActorUserID: &actorID,
		AgentRunID: &runID, ApplicationEventID: &eventID,
	})
	descriptor := descriptorFor("reply_agent_message", http.MethodPost)

	_, err := transport.DoJSON(ctx, JSONCall{
		Descriptor: descriptor,
		Path:       "/open-apis/im/v1/messages/private-message-id/reply?uuid=secret",
		Token:      "secret-access-token",
		Input:      map[string]string{"content": "private message"},
	})

	require.NoError(t, err)
	require.Equal(t, larkaudit.OutcomeSucceeded, writer.event.Outcome)
	require.Equal(t, descriptor.RoutePattern, writer.event.RoutePattern)
	require.Equal(t, "request-1", writer.event.RequestID)
	require.Equal(t, runID, *writer.event.AgentRunID)
	require.Equal(t, eventID, *writer.event.ApplicationEventID)
	require.Equal(t, &status, writer.event.HTTPStatus)
	encoded, marshalErr := json.Marshal(writer.event)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "secret-access-token")
	require.NotContains(t, string(encoded), "private-message-id")
	require.NotContains(t, string(encoded), "private message")
}

func TestAuditedTransportClassifiesProviderFailuresWithoutReplacingCallError(t *testing.T) {
	callErr := providerError(
		"send_notification", identity.ProviderRateLimited,
		"provider-log-2", errors.New("limited"),
	)
	writer := &larkAuditWriterStub{err: errors.New("audit unavailable")}
	transport := newAuditedTransport(transportStub{
		result: CallResult{HTTPStatus: http.StatusTooManyRequests}, err: callErr,
	}, writer)

	_, err := transport.DoJSON(context.Background(), JSONCall{
		Descriptor: descriptorFor("send_notification", http.MethodPost),
	})

	require.ErrorIs(t, err, identity.ErrProviderTransient)
	require.Equal(t, larkaudit.OutcomeRateLimited, writer.event.Outcome)
	require.Equal(t, string(identity.ProviderRateLimited), writer.event.ErrorCategory)
}

func TestAuditedTransportUsesCallDurationNotAuditWriteDuration(t *testing.T) {
	writer := &larkAuditWriterStub{}
	transport := newAuditedTransport(transportStub{}, writer).(*auditedTransport)
	times := []time.Time{
		time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 6, 8, 0, 0, int(25*time.Millisecond), time.UTC),
	}
	transport.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}

	_, err := transport.DoJSON(context.Background(), JSONCall{
		Descriptor: descriptorFor("get_bot_info", http.MethodGet),
	})

	require.NoError(t, err)
	require.EqualValues(t, 25, writer.event.DurationMS)
}

func TestHTTPTransportExtractsSafeProviderMetadataFromErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"code":230001,"error":{"log_id":"body-log-id"},"msg":"private detail"}`))
	}))
	t.Cleanup(server.Close)
	transport := httpTransport{baseURL: server.URL, client: server.Client()}
	var output map[string]any

	result, err := transport.DoJSON(context.Background(), JSONCall{
		Descriptor: descriptorFor("send_notification", http.MethodPost),
		Path:       "/message", Output: &output,
	})

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, result.HTTPStatus)
	require.Equal(t, 230001, *result.ProviderCode)
	require.Equal(t, "body-log-id", result.ProviderRequestID)
	require.Equal(t, "body-log-id", identity.ProviderRequestIDFromError(err))
}

func TestHTTPTransportRejectsInvalidDescriptorBeforeSending(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)
	transport := httpTransport{baseURL: server.URL, client: server.Client()}

	_, jsonErr := transport.DoJSON(context.Background(), JSONCall{Path: "/message"})
	_, downloadErr := transport.Download(context.Background(), DownloadCall{
		Path: "/artifact", Target: &bytes.Buffer{}, MaxBytes: 10,
	})

	require.Error(t, jsonErr)
	require.Error(t, downloadErr)
	require.Zero(t, requestCount)
}
