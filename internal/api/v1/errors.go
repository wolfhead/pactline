package v1

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	baseapi "github.com/wolfhead/pactline/internal/api"
	"github.com/wolfhead/pactline/internal/domain"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
)

func ErrorHandler(_ context.Context, w http.ResponseWriter, r *http.Request, err error) {
	status := ogenerrors.ErrorCode(err)
	problem := baseapi.Problem{
		Status: status, Title: "Invalid request",
		Detail: "The request does not match the API contract.", Code: "VALIDATION_FAILED",
	}
	switch {
	case errors.Is(err, ErrAuthenticationRequired):
		problem.Status, problem.Title = http.StatusUnauthorized, "Authentication required"
		problem.Detail, problem.Code = "Authentication is required.", "AUTHENTICATION_REQUIRED"
	case errors.Is(err, ErrInsufficientScope):
		problem.Status, problem.Title = http.StatusForbidden, "Insufficient scope"
		problem.Detail = "The bearer token does not grant the required scope."
		problem.Code = "INSUFFICIENT_SCOPE"
	case errors.Is(err, ErrInvalidRequest):
		problem.Status, problem.Title = http.StatusBadRequest, "Invalid request"
		problem.Detail, problem.Code = "The request is invalid.", "INVALID_REQUEST"
	case errors.Is(err, ErrPreconditionRequired):
		problem.Status, problem.Title = http.StatusPreconditionRequired, "Precondition required"
		problem.Detail = "The current resource ETag is required in If-Match."
		problem.Code = "PRECONDITION_REQUIRED"
	case errors.Is(err, domain.ErrVersionConflict):
		problem.Status, problem.Title = http.StatusPreconditionFailed, "Version conflict"
		problem.Detail = "The resource changed after the supplied ETag was issued."
		problem.Code = "VERSION_CONFLICT"
		var conflict domain.VersionConflictError
		if errors.As(err, &conflict) {
			problem.CurrentVersion = &conflict.CurrentVersion
		}
	case errors.Is(err, domain.ErrNotFound):
		problem.Status, problem.Title = http.StatusNotFound, "Not found"
		problem.Detail, problem.Code = "The requested resource was not found.", "NOT_FOUND"
	case errors.Is(err, domain.ErrForbidden):
		problem.Status, problem.Title = http.StatusForbidden, "Forbidden"
		problem.Detail, problem.Code = "The operation is not permitted.", "FORBIDDEN"
	case errors.Is(err, domain.ErrInvalidTransition):
		problem.Status, problem.Title = http.StatusConflict, "Invalid Task transition"
		problem.Detail = "The Task cannot perform that command from its current lifecycle state."
		problem.Code = "INVALID_TRANSITION"
	case errors.Is(err, domain.ErrActiveClaim):
		problem.Status, problem.Title = http.StatusConflict, "Active Claim exists"
		problem.Detail = "The Task already has an active Claim."
		problem.Code = "ACTIVE_CLAIM"
	case errors.Is(err, domain.ErrActiveIssue):
		problem.Status, problem.Title = http.StatusConflict, "Active Issue exists"
		problem.Detail = "The Task already has an open Issue Thread."
		problem.Code = "ACTIVE_ISSUE"
	case errors.Is(err, domain.ErrMigrationRequired):
		problem.Status, problem.Title = http.StatusConflict, "Task migration required"
		problem.Detail = "The Task has not been classified into the current lifecycle model."
		problem.Code = "MIGRATION_REQUIRED"
	case errors.Is(err, domain.ErrWrongIssueType):
		problem.Status, problem.Title = http.StatusBadRequest, "Unsupported Issue type"
		problem.Detail = "The Issue type is not supported by the current workflow."
		problem.Code = "WRONG_ISSUE_TYPE"
	case errors.Is(err, domain.ErrConflict):
		problem.Status, problem.Title = http.StatusConflict, "Conflict"
		problem.Detail, problem.Code = "The operation conflicts with current state.", "CONFLICT"
	case errors.Is(err, domain.ErrInvalidInput):
		problem.Status, problem.Title = http.StatusBadRequest, "Invalid request"
		problem.Detail, problem.Code = "The request is invalid.", "INVALID_REQUEST"
	case errors.Is(err, ht.ErrNotImplemented):
		problem.Status, problem.Title = http.StatusInternalServerError, "Internal server error"
		problem.Detail, problem.Code = "The request could not be completed.", "INTERNAL_ERROR"
	default:
		if status < 400 || status >= 500 {
			problem.Status, problem.Title = http.StatusInternalServerError, "Internal server error"
			problem.Detail, problem.Code = "The request could not be completed.", "INTERNAL_ERROR"
		}
	}
	if problem.Status >= 500 {
		slog.Error("v1 request failed",
			"request_id", baseapi.RequestIDFromContext(r.Context()),
			"method", r.Method, "path", r.URL.Path, "error", err)
	} else if problem.Code == "INVALID_TRANSITION" ||
		problem.Code == "ACTIVE_CLAIM" || problem.Code == "ACTIVE_ISSUE" ||
		problem.Code == "MIGRATION_REQUIRED" || problem.Code == "WRONG_ISSUE_TYPE" {
		slog.Warn("v1 workflow request rejected",
			"request_id", baseapi.RequestIDFromContext(r.Context()),
			"method", r.Method, "path", r.URL.Path,
			"status", problem.Status, "code", problem.Code)
	}
	baseapi.WriteProblem(w, r, problem)
}
