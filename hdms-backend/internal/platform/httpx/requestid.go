package httpx

import (
	"context"
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/ids"
)

type requestIDKey struct{}

// HeaderRequestID is the correlation header accepted from the client and
// echoed on every response and error body (docs/06-api-contract.md).
const HeaderRequestID = "X-Request-Id"

// WithRequestID middleware ensures every request carries an ID: the
// client's, if it sent one, otherwise a freshly generated one.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = ids.New()
		}
		w.Header().Set(HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestID returns the request ID stashed on ctx by WithRequestID, or ""
// outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
