package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestTryEntity_ReturnsErrorOnBadConfig confirms TryEntity converts a
// misconfiguration into an error rather than crashing the process — the
// property an agent-driven authoring loop needs. An invalid TenantField makes
// the underlying Define panic; TryEntity must recover it.
func TestTryEntity_ReturnsErrorOnBadConfig(t *testing.T) {
	app := NewApp()
	err := app.TryEntity("posts", entity.EntityConfig{
		MultiTenant: true,
		TenantField: "bad field; DROP TABLE", // not a valid SQL identifier
		Fields:      []schema.Field{{Name: "title", Type: schema.String}},
	})
	if err == nil {
		t.Fatal("TryEntity with invalid TenantField returned nil error")
	}
	if !strings.Contains(err.Error(), "posts") {
		t.Errorf("error should name the entity: %v", err)
	}
}

// TestTryEntity_OKOnGoodConfig confirms the happy path returns nil and
// registers the entity.
func TestTryEntity_OKOnGoodConfig(t *testing.T) {
	app := NewApp()
	if err := app.TryEntity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}); err != nil {
		t.Fatalf("TryEntity good config: %v", err)
	}
	if _, err := app.Registry.Get("posts"); err != nil {
		t.Errorf("entity not registered: %v", err)
	}
}

// TestEntity_StillPanicsOnBadConfig confirms the convenience Entity wrapper
// preserves its fail-fast panic contract.
func TestEntity_StillPanicsOnBadConfig(t *testing.T) {
	app := NewApp()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Entity with invalid config should panic")
		}
	}()
	app.Entity("posts", entity.EntityConfig{
		MultiTenant: true,
		TenantField: "bad field; DROP TABLE",
		Fields:      []schema.Field{{Name: "title", Type: schema.String}},
	})
}
