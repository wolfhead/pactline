package api

import (
	"net/http"

	"bountyboard/internal/store"
)

// TaskSurface bundles the stores the task-management endpoints need. It is
// optional (see NewRouter's variadic taskSurface parameter) purely so that
// internal/legacy/api's own test suite — which builds a router with only
// users and the legacy mux, and predates the task surface — keeps compiling
// unchanged; every real caller (cmd/server/main.go, this package's own
// tests) passes one.
type TaskSurface struct {
	Tasks    *store.TaskStore
	Comments *store.CommentStore
	Labels   *store.LabelStore
}

// NewRouter builds the top-level API handler: user listing, every mechanism
// endpoint mounted under /api/legacy/ (see legacyMux, and
// internal/legacy/README.md for why the mechanism lives there instead of at
// /api/ any more), and — when a TaskSurface is supplied — the task
// management surface itself (tasks, comments, activity, labels; see
// internal/domain/task.go). All of it is wrapped in the same identity
// middleware — legacy has no separate authentication story of its own, and
// neither does the task surface.
func NewRouter(
	users *store.UserStore,
	legacyMux http.Handler,
	taskSurface ...TaskSurface,
) http.Handler {
	mux := http.NewServeMux()

	uh := &userHandler{users: users}
	mux.HandleFunc("GET /api/users", uh.list)

	if len(taskSurface) > 0 {
		ts := taskSurface[0]
		mountTaskRoutes(mux, ts)
	}

	mux.Handle("/api/legacy/", legacyMux)

	return withIdentity(users, mux)
}

// mountTaskRoutes registers the task/comment/activity/label endpoints on
// mux. Split out of NewRouter purely for readability — it has no meaning on
// its own outside a NewRouter call.
func mountTaskRoutes(mux *http.ServeMux, ts TaskSurface) {
	th := &taskHandler{tasks: ts.Tasks}
	mux.HandleFunc("POST /api/tasks", th.create)
	mux.HandleFunc("GET /api/tasks", th.list)
	mux.HandleFunc("GET /api/tasks/{number}", th.get)
	mux.HandleFunc("PATCH /api/tasks/{number}", th.update)
	mux.HandleFunc("POST /api/tasks/{number}/archive", th.archive)
	mux.HandleFunc("POST /api/tasks/{number}/restore", th.restore)

	ch := &commentHandler{tasks: ts.Tasks, comments: ts.Comments}
	mux.HandleFunc("GET /api/tasks/{number}/comments", ch.list)
	mux.HandleFunc("POST /api/tasks/{number}/comments", ch.create)
	mux.HandleFunc("PATCH /api/tasks/{number}/comments/{id}", ch.update)
	mux.HandleFunc("DELETE /api/tasks/{number}/comments/{id}", ch.delete)

	ah := &activityHandler{tasks: ts.Tasks}
	mux.HandleFunc("GET /api/tasks/{number}/activity", ah.list)

	lh := &labelHandler{labels: ts.Labels}
	mux.HandleFunc("GET /api/labels", lh.list)
	mux.HandleFunc("POST /api/labels", lh.create)
	mux.HandleFunc("PATCH /api/labels/{id}", lh.rename)
	mux.HandleFunc("DELETE /api/labels/{id}", lh.delete)
}
