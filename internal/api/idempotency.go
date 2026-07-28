package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/identity"
)

type idempotencyRepository interface {
	Claim(
		context.Context,
		access.IdempotencyKey,
		[]byte,
		time.Time,
		time.Time,
	) (access.Claim, error)
	Complete(context.Context, access.IdempotencyKey, access.StoredResponse) error
	Release(context.Context, access.IdempotencyKey) error
}

type idempotencyMiddleware struct {
	store  idempotencyRepository
	routes routeResolver
	now    func() time.Time
}

func (m idempotencyMiddleware) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current, ok := identity.FromContext(r.Context())
		if !ok ||
			current.AuthenticationMethod != access.AuthenticationMethodAPIToken ||
			!requiresIdempotency(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if current.APITokenID == nil {
			writeIdempotencyInternalError(w, r, "bearer identity has no token ID", nil)
			return
		}
		if m.store == nil {
			writeIdempotencyInternalError(w, r, "idempotency store is unavailable", nil)
			return
		}
		values := r.Header.Values("Idempotency-Key")
		if len(values) == 0 || values[0] == "" {
			WriteProblem(w, r, Problem{
				Title: "Idempotency key required", Status: http.StatusBadRequest,
				Detail: "Bearer mutations require an Idempotency-Key header.",
				Code:   "IDEMPOTENCY_KEY_REQUIRED",
			})
			return
		}
		if len(values) != 1 || !validIdempotencyKey(values[0]) {
			WriteProblem(w, r, Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "Idempotency-Key must contain 1 to 128 visible non-space ASCII characters.",
				Code:   "INVALID_REQUEST",
			})
			return
		}
		route := ""
		if m.routes != nil {
			_, pattern := m.routes.Handler(r)
			route = stripMethodPattern(pattern)
		}
		if route == "" {
			writeIdempotencyInternalError(w, r, "canonical route pattern is unavailable", nil)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteProblem(w, r, Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "The request body could not be read.", Code: "INVALID_REQUEST",
			})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		requestHash := idempotencyFingerprint(r, route, body)
		key := access.IdempotencyKey{
			UserID: current.Actor.ID, TokenID: *current.APITokenID,
			Method: r.Method, RoutePattern: route, Value: values[0],
		}
		now := time.Now().UTC()
		if m.now != nil {
			now = m.now().UTC()
		}
		claim, err := m.store.Claim(
			r.Context(), key, requestHash, now, now.Add(access.IdempotencyRetention),
		)
		if err != nil {
			writeIdempotencyInternalError(w, r, "claim idempotency key", err)
			return
		}
		switch claim.Kind {
		case access.ClaimReplay:
			replayStoredResponse(w, r, claim.Response)
			return
		case access.ClaimInProgress:
			w.Header().Set("Retry-After", "1")
			retryable := true
			WriteProblem(w, r, Problem{
				Title: "Request in progress", Status: http.StatusConflict,
				Detail: "A request with this idempotency key is still in progress.",
				Code:   "IDEMPOTENCY_IN_PROGRESS", Retryable: &retryable,
			})
			return
		case access.ClaimReused:
			WriteProblem(w, r, Problem{
				Title: "Idempotency key reused", Status: http.StatusConflict,
				Detail: "This idempotency key was already used for a different request.",
				Code:   "IDEMPOTENCY_KEY_REUSED",
			})
			return
		case access.ClaimAcquired:
		default:
			writeIdempotencyInternalError(w, r, "unknown idempotency claim outcome", nil)
			return
		}

		captured := newCapturedResponse()
		defer func() {
			if recovered := recover(); recovered != nil {
				if releaseErr := m.store.Release(r.Context(), key); releaseErr != nil {
					slog.Error("release idempotency claim after panic failed",
						"request_id", requestID(r), "route", route, "error", releaseErr)
				}
				panic(recovered)
			}
		}()
		next.ServeHTTP(captured, r)
		response := captured.storedResponse(requestID(r))
		if response.StatusCode >= 500 {
			if err := m.store.Release(r.Context(), key); err != nil {
				slog.Error("release idempotency claim after server error failed",
					"request_id", requestID(r), "route", route, "error", err)
			}
			captured.writeTo(w)
			return
		}
		if err := m.store.Complete(r.Context(), key, response); err != nil {
			writeIdempotencyInternalError(w, r, "complete idempotency record", err)
			return
		}
		captured.writeTo(w)
	})
}

func requiresIdempotency(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validIdempotencyKey(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func idempotencyFingerprint(r *http.Request, route string, body []byte) []byte {
	hash := sha256.New()
	for _, value := range []string{
		r.Method,
		route,
		r.URL.Query().Encode(),
		strings.TrimSpace(r.Header.Get("Content-Type")),
	} {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	_, _ = hash.Write(body)
	return hash.Sum(nil)
}

func replayStoredResponse(w http.ResponseWriter, r *http.Request, response access.StoredResponse) {
	for name, values := range response.Headers {
		if strings.EqualFold(name, "X-Request-ID") {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.Header().Set("X-Request-ID", requestID(r))
	w.Header().Set("Idempotency-Replayed", "true")
	markIdempotencyReplayed(r)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}

func markIdempotencyReplayed(r *http.Request) {
	state, _ := r.Context().Value(accessAuditContextKey{}).(*accessAuditState)
	if state != nil {
		state.idempotencyReplayed = true
	}
}

func writeIdempotencyInternalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	attributes := []any{"request_id", requestID(r), "operation", message}
	if err != nil {
		attributes = append(attributes, "error", err)
	}
	slog.Error("idempotency processing failed", attributes...)
	WriteProblem(w, r, Problem{
		Title: "Internal server error", Status: http.StatusInternalServerError,
		Detail: "The request could not be completed.", Code: "INTERNAL_ERROR",
	})
}

var replayedResponseHeaders = map[string]bool{
	"Content-Type": true,
	"Etag":         true,
	"Location":     true,
	"X-Request-Id": true,
}

type capturedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCapturedResponse() *capturedResponse {
	return &capturedResponse{header: make(http.Header)}
}

func (w *capturedResponse) Header() http.Header { return w.header }

func (w *capturedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *capturedResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *capturedResponse) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *capturedResponse) storedResponse(currentRequestID string) access.StoredResponse {
	headers := make(map[string][]string)
	for name, values := range w.header {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if replayedResponseHeaders[canonical] {
			headers[canonical] = append([]string(nil), values...)
		}
	}
	headers["X-Request-Id"] = []string{currentRequestID}
	return access.StoredResponse{
		StatusCode: w.statusCode(), Headers: headers,
		Body: append([]byte(nil), w.body.Bytes()...),
	}
}

func (w *capturedResponse) writeTo(target http.ResponseWriter) {
	for name, values := range w.header {
		for _, value := range values {
			target.Header().Add(name, value)
		}
	}
	target.WriteHeader(w.statusCode())
	_, _ = target.Write(w.body.Bytes())
}

var _ http.ResponseWriter = (*capturedResponse)(nil)
