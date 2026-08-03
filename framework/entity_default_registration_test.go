package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Issue #174, the reported case verbatim: a JSON field whose Default is not
// JSON used to be accepted at registration and then 500 on any create that
// omitted the field (Postgres JSONB rejects `draft`) while silently storing
// the garbage on SQLite. It must be refused at registration instead, naming
// the field.
func TestEntityRejectsMalformedJSONDefault(t *testing.T) {
	app := atomicTestApp(t)

	err := app.TryEntity("things", EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "flags", Type: schema.JSON, Default: "draft"},
		},
	})
	if err == nil {
		t.Fatal(`TryEntity accepted flags: schema.JSON with Default "draft" — POST /things omitting "flags" now 500s against a Postgres JSONB column (invalid input syntax for type json) and silently stores "draft" in SQLite's TEXT column, while a caller who SENDS "draft" gets a 400 naming the field`)
	}
	if !strings.Contains(err.Error(), "flags") {
		t.Errorf("error must name the offending field, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Default") {
		t.Errorf("error must point at the Default, got: %v", err)
	}
	if _, gerr := app.Registry.Get("things"); gerr == nil {
		t.Error("rejected entity must not remain in the registry")
	}
}

// Entity is the panicking variant; TryEntity callers get the error. Both must
// fail the same declaration.
func TestEntityPanicsOnMalformedDefault(t *testing.T) {
	app := atomicTestApp(t)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal(`Entity accepted flags: schema.JSON with Default "draft" — the bad declaration boots, and every create that omits "flags" fails at the driver instead`)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "flags") {
			t.Fatalf("panic = %v, want it to name %q", r, "flags")
		}
	}()
	app.Entity("things", EntityConfig{
		Fields: []schema.Field{{Name: "flags", Type: schema.JSON, Default: "draft"}},
	})
}
