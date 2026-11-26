package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ent-1: Registry.Register must run Entity.Validate so misconfigs surface at
// registration time with the actionable message, not as an opaque SQL error
// three phases later.
func TestRegisterRejectsInvalidConfig(t *testing.T) {
	reg := NewRegistry()

	// Two fields named "title", a duplicate-field misconfig that Validate
	// catches but Define does not inject away.
	e := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
	})

	err := reg.Register(e)
	if err == nil {
		t.Fatalf("expected Register to reject invalid entity config, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Errorf("expected actionable validation message about duplicate field, got %q", err.Error())
	}
}

func TestRegistryZeroValueRegistersEntity(t *testing.T) {
	var registry Registry
	ent := Define("Task", EntityConfig{})
	if err := registry.Register(ent); err != nil {
		t.Fatal(err)
	}
	if got, err := registry.Get("Task"); got != ent || err != nil {
		t.Fatalf("Get returned %p, %v, want %p", got, err, ent)
	}
}
