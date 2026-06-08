package framework

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
)

// TestDBFromContext_RoundTrips asserts the context DB accessor returns the
// handle stamped via WithDBContext, giving screens a package-portable way
// to reach the app's *sql.DB through ctx instead of a package-level global.
func TestDBFromContext_RoundTrips(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })

	// Nothing stamped → not found.
	if _, ok := DBFromContext(context.Background()); ok {
		t.Fatal("DBFromContext on bare ctx should report not-found")
	}

	ctx := WithDBContext(context.Background(), db)
	got, ok := DBFromContext(ctx)
	if !ok {
		t.Fatal("DBFromContext after WithDBContext: not found")
	}
	if got != db {
		t.Fatal("DBFromContext returned a different *sql.DB")
	}
}

// TestAppInjectsDBContext asserts that an App with a DB exposes a
// middleware that stamps the handle into every request context, so a
// screen handler can pull it without a global.
func TestAppInjectsDBContext(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())

	var seen *sql.DB
	var sawValue bool
	app.Use(app.DBContextMiddleware())
	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, sawValue = DBFromContext(r.Context())
	}))

	ta := TestHarness(t, app)
	ta.Get("/probe").AssertStatus(t, 200)

	if !sawValue || seen != db {
		t.Fatalf("handler did not see the app DB via context (ok=%v)", sawValue)
	}
}
