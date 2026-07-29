package v1

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	baseapi "bountyboard/internal/api"
	"bountyboard/internal/domain"

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
	case errors.Is(err, domain.ErrNotFound):
		problem.Status, problem.Title = http.StatusNotFound, "Not found"
		problem.Detail, problem.Code = "The requested resource was not found.", "NOT_FOUND"
	case errors.Is(err, domain.ErrForbidden):
		problem.Status, problem.Title = http.StatusForbidden, "Forbidden"
		problem.Detail, problem.Code = "The operation is not permitted.", "FORBIDDEN"
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
	}
	baseapi.WriteProblem(w, r, problem)
}
