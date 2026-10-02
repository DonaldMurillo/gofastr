package renderdiag

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

// TestResponse marks recovered render failures as HTTP 500 in test binaries.
// Use only for finite render responses whose rendering precedes the first write,
// never for SSE or other streams that commit headers before rendering completes.
// Production keeps its writer, request, and recovery status unchanged.
func TestResponse(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	if !testing.Testing() {
		return w, r
	}
	if allowed, _ := r.Context().Value(productionStatusKey{}).(*atomic.Bool); allowed != nil && allowed.Load() {
		return w, r
	}
	out := &testResponse{ResponseWriter: w}
	previous, _ := r.Context().Value(observerKey{}).(func(string))
	ctx := WithObserver(r.Context(), func(message string) {
		out.failed.Store(true)
		if previous != nil {
			previous(message)
		}
	})
	return out, r.WithContext(ctx)
}

type productionStatusKey struct{}

// ProductionStatus scopes production recovery status to a test-owned handler.
func ProductionStatus(t testing.TB, next http.Handler) http.Handler {
	t.Helper()
	if !testing.Testing() {
		panic("renderdiag: production status exemption requires a test binary")
	}
	active := new(atomic.Bool)
	active.Store(true)
	t.Cleanup(func() { active.Store(false) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), productionStatusKey{}, active)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type testResponse struct {
	http.ResponseWriter
	failed      atomic.Bool
	wroteHeader bool
}

func (w *testResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if w.failed.Load() {
		status = http.StatusInternalServerError
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *testResponse) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

// Unwrap exposes the underlying writer so http.NewResponseController
// reaches the real writer's capabilities instead of failing on the
// wrapper. Without it a Flush (or any future controller extension)
// would silently no-op only in test binaries.
func (w *testResponse) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush delegates when the underlying writer supports it, so streamed
// responses keep streaming under the test wrapper. The 500-on-panic
// contract is unaffected: these responses commit headers only after
// rendering completes.
func (w *testResponse) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
