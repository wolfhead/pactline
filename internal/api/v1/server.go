package v1

import (
	"net/http"

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
