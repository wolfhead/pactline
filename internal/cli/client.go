package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type APIError struct {
	Status    int    `json:"status,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Key       string `json:"idempotency_key,omitempty"`
}

func (e *APIError) Error() string { return e.Message }

type client struct {
	server, token, clientKind, sessionID string
	httpClient                           *http.Client
	verbose                              func(string, ...any)
	lastMeta                             responseMeta
}

type responseMeta struct {
	RequestID      string `json:"request_id,omitempty"`
	ETag           string `json:"etag,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (m responseMeta) empty() bool {
	return m.RequestID == "" && m.ETag == "" && m.IdempotencyKey == ""
}

func (c *client) request(
	ctx context.Context,
	method, path string,
	body any,
	taskVersion int64,
	idempotencyKey string,
	mutation bool,
) (json.RawMessage, http.Header, error) {
	c.lastMeta = responseMeta{}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	endpoint, err := url.JoinPath(c.server, path)
	if err != nil {
		return nil, nil, fmt.Errorf("build request URL: %w", err)
	}
	if strings.Contains(path, "?") {
		base, query, _ := strings.Cut(path, "?")
		endpoint, err = url.JoinPath(c.server, base)
		if err == nil {
			endpoint += "?" + query
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if taskVersion > 0 {
		request.Header.Set("If-Match", fmt.Sprintf(`"%d"`, taskVersion))
	}
	if mutation {
		if idempotencyKey == "" {
			idempotencyKey = uuid.NewString()
		}
		request.Header.Set("Idempotency-Key", idempotencyKey)
		request.Header.Set("Pactline-Client-Kind", c.clientKind)
		request.Header.Set("Pactline-Client-Session-ID", c.sessionID)
	}
	started := time.Now()
	c.verbose("request method=%s path=%s", method, strings.Split(path, "?")[0])
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, nil, &APIError{
			Code: "NETWORK_ERROR", Message: "The request outcome is unknown: " + err.Error(),
			Hint: "Inspect current state or repeat the exact command with the same idempotency key.",
			Key:  idempotencyKey,
		}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, response.Header, fmt.Errorf("read response: %w", err)
	}
	c.verbose("response status=%d duration=%s request_id=%s etag=%s",
		response.StatusCode, time.Since(started).Round(time.Millisecond),
		response.Header.Get("X-Request-ID"), response.Header.Get("ETag"))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		problem := struct {
			Code      string `json:"code"`
			Detail    string `json:"detail"`
			Title     string `json:"title"`
			RequestID string `json:"request_id"`
		}{Code: "HTTP_ERROR", Detail: response.Status}
		_ = json.Unmarshal(responseBody, &problem)
		message := problem.Detail
		if message == "" {
			message = problem.Title
		}
		if message == "" {
			message = response.Status
		}
		requestID := problem.RequestID
		if requestID == "" {
			requestID = response.Header.Get("X-Request-ID")
		}
		return nil, response.Header, &APIError{
			Status: response.StatusCode, Code: problem.Code, Message: message,
			RequestID: requestID, Key: idempotencyKey,
		}
	}
	c.lastMeta = responseMeta{
		RequestID: response.Header.Get("X-Request-ID"), ETag: response.Header.Get("ETag"),
		IdempotencyKey: idempotencyKey,
	}
	return json.RawMessage(responseBody), response.Header, nil
}
