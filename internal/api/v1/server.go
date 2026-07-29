package v1

import (
	"net/http"

	baseapi "bountyboard/internal/api"
	generated "bountyboard/internal/api/v1generated"
)

type Server struct {
	generated *generated.Server
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
	return &Server{generated: transport}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	case "archiveProject",
		"archiveTask",
		"cancelMilestone",
		"cancelProject",
		"completeMilestone",
		"completeProject",
		"createAcceptanceCheck",
		"createMilestone",
		"createMilestoneCriterion",
		"createProjectCriterion",
		"createTaskComment",
		"createTaskCriterion",
		"deleteCriterion",
		"deleteLabel",
		"deleteTaskComment",
		"pauseProject",
		"reopenMilestone",
		"reopenProject",
		"restoreProject",
		"restoreTask",
		"updateCriterion",
		"updateLabel",
		"updateMilestone",
		"updateProject",
		"updateTask",
		"updateTaskComment":
		return true
	default:
		return false
	}
}

func operationRequiresProjectIfMatch(operationID string) bool {
	switch operationID {
	case "cancelMilestone",
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
