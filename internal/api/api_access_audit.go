package api

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/access"

	"github.com/google/uuid"
)

type accessAuditWriter interface {
	RecordAccessAudit(context.Context, access.RequestAuditEvent) error
}

type routeResolver interface {
	Handler(*http.Request) (http.Handler, string)
}

type apiAccessAudit struct {
	store  accessAuditWriter
	now    func() time.Time
	routes routeResolver
}

type accessAuditState struct {
	authMethod          access.AuthenticationMethod
	authOutcome         access.AuthOutcome
	userID              *uuid.UUID
	tokenID             *uuid.UUID
	tokenName           string
	agentRunID          *uuid.UUID
	problemCode         string
	idempotencyReplayed bool
}

type accessAuditContextKey struct{}

func (m apiAccessAudit) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := m.now
		if now == nil {
			now = time.Now
		}
		started := now().UTC()
		state := &accessAuditState{
			authMethod:  access.AuthenticationMethodUnknown,
			authOutcome: access.AuthOutcomeRejected,
		}
		ctx := context.WithValue(r.Context(), accessAuditContextKey{}, state)
		auditedRequest := r.WithContext(ctx)
		recorder := &auditResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, auditedRequest)

		if m.store == nil {
			return
		}
		statusCode := recorder.statusCode()
		// Successful reads are ordinary UI and Agent traffic, not durable
		// security evidence. Mutations and every rejected or failed request
		// remain individually auditable.
		if r.Method == http.MethodGet && statusCode >= 200 && statusCode < 400 {
			return
		}
		route := "unmatched"
		if m.routes != nil {
			_, pattern := m.routes.Handler(auditedRequest)
			if pattern != "" {
				route = stripMethodPattern(pattern)
			}
		}
		event := access.RequestAuditEvent{
			ID: uuid.New(), OccurredAt: started, RequestID: requestID(auditedRequest),
			AuthMethod: state.authMethod, AuthOutcome: state.authOutcome,
			UserID: state.userID, TokenID: state.tokenID, TokenName: state.tokenName,
			AgentRunID: state.agentRunID,
			Method:     r.Method, RoutePattern: route, StatusCode: statusCode,
			ProblemCode:   state.problemCode,
			DurationMS:    max(0, now().UTC().Sub(started).Milliseconds()),
			ResponseBytes: recorder.bytes, IdempotencyReplayed: state.idempotencyReplayed,
			UserAgent: r.UserAgent(), NetworkAddress: networkAddress(r.RemoteAddr),
		}
		// The client may disconnect or cancel navigation before the handler
		// returns. Access audit is the durable record of that completed
		// attempt, so give its bounded write an independent cancellation
		// lifetime while preserving request-scoped values.
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		if err := m.store.RecordAccessAudit(auditCtx, event); err != nil {
			slog.Error("record API access audit failed",
				"request_id", event.RequestID, "method", event.Method,
				"route", event.RoutePattern, "status", event.StatusCode, "error", err)
		}
	})
}

func markAccessAuthentication(
	r *http.Request,
	method access.AuthenticationMethod,
	outcome access.AuthOutcome,
	userID, tokenID, agentRunID *uuid.UUID,
	tokenName string,
) {
	state, _ := r.Context().Value(accessAuditContextKey{}).(*accessAuditState)
	if state == nil {
		return
	}
	state.authMethod, state.authOutcome = method, outcome
	state.userID, state.tokenID, state.tokenName = userID, tokenID, tokenName
	state.agentRunID = agentRunID
}

func markProblemCode(r *http.Request, code string) {
	state, _ := r.Context().Value(accessAuditContextKey{}).(*accessAuditState)
	if state != nil {
		state.problemCode = code
	}
}

func stripMethodPattern(pattern string) string {
	if _, route, found := strings.Cut(pattern, " "); found {
		return route
	}
	return pattern
}

func networkAddress(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return host
	}
	if net.ParseIP(remote) != nil {
		return remote
	}
	return ""
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *auditResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

func (w *auditResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *auditResponseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *auditResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *auditResponseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (w *auditResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(reader)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(struct{ io.Writer }{w.ResponseWriter}, reader)
	w.bytes += n
	return n, err
}

var _ http.Flusher = (*auditResponseWriter)(nil)
var _ http.Hijacker = (*auditResponseWriter)(nil)
var _ http.Pusher = (*auditResponseWriter)(nil)
var _ io.ReaderFrom = (*auditResponseWriter)(nil)
