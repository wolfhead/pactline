package v1

import (
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/wolfhead/pactline/internal/access"
	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

type Server struct {
	generated *generated.Server
	handler   *Handler
}

func NewServer(handler *Handler) (*Server, error) {
	transport, err := generated.NewServer(
		handler,
		Security{},
		generated.WithErrorHandler(ErrorHandler),
	)
	if err != nil {
		return nil, err
	}
	return &Server{generated: transport, handler: handler}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.serveAttachmentContent(w, r) {
		return
	}
	route, routeFound := s.generated.FindPath(r.Method, r.URL)
	if routeFound && operationRequiresIfMatch(route.OperationID()) {
		values := r.Header.Values("If-Match")
		if len(values) == 0 || values[0] == "" {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Precondition required", Status: http.StatusPreconditionRequired,
				Detail: "The current resource ETag is required in If-Match.",
				Code:   "PRECONDITION_REQUIRED",
			})
			return
		}
		if len(values) != 1 {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "If-Match must contain one quoted positive integer.",
				Code:   "INVALID_REQUEST",
			})
			return
		}
		if _, err := parseIfMatch(values[0]); err != nil {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "If-Match must contain one quoted positive integer.",
				Code:   "INVALID_REQUEST",
			})
			return
		}
	}
	if routeFound && operationRequiresProjectIfMatch(route.OperationID()) {
		values := r.Header.Values("X-Project-If-Match")
		if len(values) == 0 || values[0] == "" {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Precondition required", Status: http.StatusPreconditionRequired,
				Detail: "The current project ETag is required in X-Project-If-Match.",
				Code:   "PRECONDITION_REQUIRED",
			})
			return
		}
		if len(values) != 1 {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "X-Project-If-Match must contain one quoted positive integer.",
				Code:   "INVALID_REQUEST",
			})
			return
		}
		if _, err := parseIfMatch(values[0]); err != nil {
			baseapi.WriteProblem(w, r, baseapi.Problem{
				Title: "Invalid request", Status: http.StatusBadRequest,
				Detail: "X-Project-If-Match must contain one quoted positive integer.",
				Code:   "INVALID_REQUEST",
			})
			return
		}
	}
	s.generated.ServeHTTP(w, r)
}

func (s *Server) serveAttachmentContent(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "tasks" || parts[4] != "attachments" || parts[len(parts)-1] != "content" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		return false
	}
	taskNumber, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || taskNumber < 1 {
		ErrorHandler(r.Context(), w, r, ErrInvalidRequest)
		return true
	}
	current, ok := identity.FromContext(r.Context())
	if !ok || !current.Subject.Active {
		ErrorHandler(r.Context(), w, r, ErrAuthenticationRequired)
		return true
	}
	requiredScope := access.ScopeWorkRead
	permission := application.ProjectPermissionRead
	if r.Method == http.MethodPut {
		requiredScope = access.ScopeWorkWrite
		permission = application.ProjectPermissionWrite
	}
	if current.AuthenticationMethod == access.AuthenticationMethodAPIToken || current.AuthenticationMethod == access.AuthenticationMethodAgentDelegate {
		if !(access.Principal{Scopes: current.Scopes}).HasScope(requiredScope) {
			ErrorHandler(r.Context(), w, r, ErrInsufficientScope)
			return true
		}
	}
	subject := domain.ProjectAccessSubject{UserID: current.Subject.ID, PlatformRole: current.Subject.PlatformRole}
	task, err := s.handler.Access.RequireTaskByNumber(r.Context(), taskNumber, subject, permission)
	if err != nil {
		ErrorHandler(r.Context(), w, r, err)
		return true
	}
	if r.Method == http.MethodPut && len(parts) == 8 && parts[5] == "uploads" {
		sessionID, parseErr := uuid.Parse(parts[6])
		if parseErr != nil {
			ErrorHandler(r.Context(), w, r, ErrInvalidRequest)
			return true
		}
		session, getErr := s.handler.Attachments.Attachments.GetUploadSession(r.Context(), sessionID)
		if getErr != nil {
			ErrorHandler(r.Context(), w, r, getErr)
			return true
		}
		if session.TaskID != task.Task.ID {
			ErrorHandler(r.Context(), w, r, domain.ErrNotFound)
			return true
		}
		if r.ContentLength < 0 {
			ErrorHandler(r.Context(), w, r, fmt.Errorf("%w: Content-Length is required", domain.ErrInvalidInput))
			return true
		}
		if uploadErr := s.handler.Attachments.UploadLocal(
			r.Context(), session, current.Subject.ID, r.Body, r.ContentLength,
		); uploadErr != nil {
			ErrorHandler(r.Context(), w, r, uploadErr)
			return true
		}
		w.Header().Set("X-Request-Id", baseapi.RequestIDFromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	if r.Method == http.MethodGet && len(parts) == 7 {
		attachmentID, parseErr := uuid.Parse(parts[5])
		if parseErr != nil {
			ErrorHandler(r.Context(), w, r, ErrInvalidRequest)
			return true
		}
		attachment, getErr := s.handler.Attachments.Attachments.Get(r.Context(), task.Task.ID, attachmentID)
		if getErr != nil {
			ErrorHandler(r.Context(), w, r, getErr)
			return true
		}
		body, openErr := s.handler.Attachments.Open(r.Context(), attachment)
		if openErr != nil {
			ErrorHandler(r.Context(), w, r, openErr)
			return true
		}
		defer body.Close()
		previewKind := domain.AttachmentPreview(attachment.Filename, attachment.MediaType)
		disposition := "inline"
		if r.URL.Query().Get("disposition") == "attachment" || previewKind == domain.AttachmentPreviewDownload {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.Filename}))
		w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
		w.Header().Set("Content-Type", attachment.MediaType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Request-Id", baseapi.RequestIDFromContext(r.Context()))
		if previewKind == domain.AttachmentPreviewHTML && disposition == "inline" {
			w.Header().Set("Content-Security-Policy", "sandbox allow-scripts")
		}
		w.WriteHeader(http.StatusOK)
		if _, copyErr := io.Copy(w, body); copyErr != nil {
			slog.WarnContext(r.Context(), "stream task attachment content",
				"task_number", taskNumber,
				"attachment_id", attachment.ID,
				"request_id", baseapi.RequestIDFromContext(r.Context()),
				"error", copyErr,
			)
			return true
		}
		return true
	}
	return false
}

// Handler adapts ogen route discovery to the canonical route resolver used by
// API access auditing and idempotency. The returned handler is informational;
// both middleware consumers rely on the stable method-and-path pattern.
func (s *Server) Handler(r *http.Request) (http.Handler, string) {
	route, ok := s.generated.FindPath(r.Method, r.URL)
	if !ok {
		return nil, ""
	}
	return s, r.Method + " " + route.PathPattern()
}

func operationRequiresIfMatch(operationID string) bool {
	switch operationID {
	case "acceptTask",
		"cancelTask",
		"createTaskStageClaim",
		"deleteTaskThreadMessage",
		"markTaskReady",
		"recordTaskStageAcceptanceCheck",
		"releaseTaskStageClaim",
		"requestTaskChanges",
		"requestTaskResolution",
		"resolveTaskIssue",
		"submitTaskWork",
		"updateTaskThreadMessage",
		"withdrawTaskReadiness",
		"activateMilestone",
		"archiveProject",
		"archiveTask",
		"cancelMilestone",
		"completeMilestone",
		"createAcceptanceCheck",
		"createMilestone",
		"createMilestoneCriterion",
		"completeTaskAttachmentUpload",
		"deleteTaskAttachment",
		"createTaskCriterion",
		"deleteCriterion",
		"deleteLabel",
		"reopenMilestone",
		"restoreProject",
		"restoreTask",
		"updateCriterion",
		"updateLabel",
		"updateMilestone",
		"updateAgentConversation",
		"updateCurrentAgentConversationConfiguration",
		"updateProject",
		"updateTask":
		return true
	default:
		return false
	}
}

func operationRequiresProjectIfMatch(operationID string) bool {
	switch operationID {
	case "activateMilestone",
		"cancelMilestone",
		"completeMilestone",
		"createMilestoneCriterion",
		"reopenMilestone",
		"updateMilestone":
		return true
	default:
		return false
	}
}

func OpenAPIHandler(document []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(document)
		}
	})
}
