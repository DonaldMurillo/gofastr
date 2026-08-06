package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWithDBContextNilDBIsNoOp pins the UI-only-app contract: stamping a nil
// *sql.DB into the context would put a nil pointer under a truthy key, so
// WithDBContext returns the context UNCHANGED and DBFromContext reports "no
// DB". A screen on a no-database app must never receive a nil *sql.DB it
// would dereference.
func TestWithDBContextNilDBIsNoOp(t *testing.T) {
	ctx := context.Background()
	out := WithDBContext(ctx, nil)
	if out != ctx {
		t.Fatal("WithDBContext(ctx, nil) returned a derived context; it must be a no-op")
	}
	if db, ok := DBFromContext(out); ok || db != nil {
		t.Fatalf("DBFromContext = %v, ok=%v after a nil stamp; want nil, false", db, ok)
	}
}

// TestWithDBContextRoundTrips verifies the positive path: a real *sql.DB
// stamped in is retrievable downstream — the package-portable alternative to a
// global handle.
func TestWithDBContextRoundTrips(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver not available: %v", err)
	}
	defer db.Close()

	ctx := WithDBContext(context.Background(), db)
	got, ok := DBFromContext(ctx)
	if !ok || got != db {
		t.Fatalf("DBFromContext = %v, ok=%v; want the stamped db, true", got, ok)
	}
}

// TestAppDBContextMiddlewarePassThroughWithoutDB: an app built without WithDB
// returns a pass-through middleware (the bare next handler, unwrapped) so a
// request never carries a nil DB in context. This is the default-middleware
// opt-out path — apps that wire their own chain still get a safe no-op.
func TestAppDBContextMiddlewarePassThroughWithoutDB(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware()) // no WithDB
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := DBFromContext(r.Context()); ok {
			t.Error("DBFromContext returned ok on a no-DB app's request")
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := app.DBContextMiddleware()(next)
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("pass-through middleware did not invoke the handler")
	}
}
