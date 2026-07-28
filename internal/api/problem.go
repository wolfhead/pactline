package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ValidationProblem struct {
	Pointer string `json:"pointer"`
	Detail  string `json:"detail"`
}

type Problem struct {
	Type           string              `json:"type"`
	Title          string              `json:"title"`
	Status         int                 `json:"status"`
	Detail         string              `json:"detail"`
	Instance       string              `json:"instance"`
	Code           string              `json:"code"`
	RequestID      string              `json:"request_id"`
	Errors         []ValidationProblem `json:"errors,omitempty"`
	CurrentVersion *int64              `json:"current_version,omitempty"`
	Retryable      *bool               `json:"retryable,omitempty"`
}

func WriteProblem(w http.ResponseWriter, r *http.Request, problem Problem) {
	markProblemCode(r, problem.Code)
	if problem.Type == "" {
		problem.Type = "about:blank"
	}
	if problem.Instance == "" {
		problem.Instance = r.URL.Path
	}
	problem.RequestID = requestID(r)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.Error("encode problem response",
			"request_id", problem.RequestID, "status", problem.Status, "code", problem.Code, "error", err)
	}
}
