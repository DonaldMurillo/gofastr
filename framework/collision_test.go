package framework

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestEntityScreenCollisionMsg asserts that registering an entity whose
// CRUD mount path collides with an existing screen/route produces an
// actionable diagnostic that names the entity, the colliding path, and a
// fix — not the opaque "/foods/llm.md conflicts with pattern" mux panic.
func TestEntityScreenCollisionMsg(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	// A screen lives at /foods.
	app.Router().Get("/foods", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	err = app.TryEntity("foods", entity.EntityConfig{
		Table:  "foods",
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	if err == nil {
		t.Fatal("expected a collision error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"foods", "/foods"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	// Must point at a fix, not just report the clash.
	if !strings.Contains(msg, "APIPrefix") && !strings.Contains(msg, "different") {
		t.Fatalf("message %q does not suggest a fix", msg)
	}
}
