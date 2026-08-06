package api

import (
	"net/http"
	"regexp"

	"github.com/wolfhead/pactline/internal/larkaudit"

	"github.com/google/uuid"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepted := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(accepted) {
			accepted = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", accepted)
		ctx := withRequestID(r.Context(), accepted)
		ctx = larkaudit.WithCorrelation(ctx, larkaudit.Correlation{RequestID: accepted})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
