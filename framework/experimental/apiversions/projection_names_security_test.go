package apiversions_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

// TestApplyToEntityConfigRefusesUnknownFieldNames pins that a
// projection naming a field the entity does not declare fails loudly at
// startup instead of being ignored. Exclude is how a version hides a
// column (an internal note, a secret) from its wire output; a name that
// matched nothing, a typo or the column's camelCase wire key instead of
// its field name, left the column visible on that version with no sign
// anything was wrong.
func TestApplyToEntityConfigRefusesUnknownFieldNames(t *testing.T) {
	base := entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "title", Type: schema.String},
		{Name: "internal_notes", Type: schema.String},
	}}
	cases := map[string]*apiversions.Projection{
		"exclude typo":      {Version: "v1", Exclude: []string{"internal_note"}},
		"exclude wire name": {Version: "v1", Exclude: []string{"internalNotes"}},
		"include typo":      {Version: "v1", Include: []string{"id", "titel"}},
		"rename typo":       {Version: "v1", Rename: map[string]string{"titel": "headline"}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ps := apiversions.NewProjectionSet(p)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("ApplyToEntityConfig accepted a projection naming an undeclared field")
				}
				if !strings.Contains(r.(string), "apiversions:") {
					t.Fatalf("panic = %v, want an apiversions message", r)
				}
			}()
			_ = apiversions.ApplyToEntityConfig(base, ps, "v1")
		})
	}

	// A projection naming declared fields still applies.
	ps := apiversions.NewProjectionSet(&apiversions.Projection{Version: "v1", Exclude: []string{"internal_notes"}})
	cfg := apiversions.ApplyToEntityConfig(base, ps, "v1")
	for _, f := range cfg.Fields {
		if f.Hidden != (f.Name == "internal_notes") {
			t.Errorf("field %s Hidden = %v", f.Name, f.Hidden)
		}
	}
}

// The name check runs on the base config, before entity.Define adds the
// columns it injects (the id key, created_at/updated_at, and the tenant,
// owner and soft-delete scope columns), so a projection naming one of
// them is naming a column the entity has. Include: {"id", "title"} panicked
// after the check landed, and Exclude: {"updated_at"} panicked with the
// wrong hint instead of hiding the column. Each is accepted, Exclude and
// Rename take effect on the column Define injects, and a name the entity
// will not have still panics.
func TestProjectionNamingInjectedColumns(t *testing.T) {
	base := entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "internal_notes", Type: schema.String}},
		Scope:  &entity.ScopeConfig{MultiTenant: true, OwnerField: "user_id", SoftDelete: true},
	}
	define := func(t *testing.T, p *apiversions.Projection) (e *entity.Entity) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("projection %+v panicked: %v", *p, r)
			}
		}()
		return entity.Define("posts", apiversions.ApplyToEntityConfig(base, apiversions.NewProjectionSet(p), "v1"))
	}
	field := func(t *testing.T, e *entity.Entity, name string) schema.Field {
		t.Helper()
		for _, f := range e.Config.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("defined entity has no %s column", name)
		return schema.Field{}
	}

	t.Run("include id", func(t *testing.T) {
		e := define(t, &apiversions.Projection{Version: "v1", Include: []string{"id", "title"}})
		if field(t, e, "id").Hidden || field(t, e, "title").Hidden || !field(t, e, "internal_notes").Hidden {
			t.Errorf("Include id,title shaped the fields wrong: %+v", e.Config.Fields)
		}
	})
	t.Run("exclude updated_at", func(t *testing.T) {
		e := define(t, &apiversions.Projection{Version: "v1", Exclude: []string{"updated_at"}})
		if !field(t, e, "updated_at").Hidden {
			t.Error("Exclude updated_at left the injected column on the wire")
		}
		if f := field(t, e, "updated_at"); f.AutoGenerate != schema.AutoTimestamp || !f.ReadOnly {
			t.Errorf("the excluded updated_at lost what Define injects: %+v", f)
		}
		if field(t, e, "created_at").Hidden || field(t, e, "title").Hidden {
			t.Error("Exclude updated_at hid another column")
		}
	})
	t.Run("rename created_at", func(t *testing.T) {
		e := define(t, &apiversions.Projection{Version: "v1", Rename: map[string]string{"created_at": "createdOn"}})
		if got := field(t, e, "created_at").WireName; got != "createdOn" {
			t.Errorf("created_at WireName = %q, want createdOn", got)
		}
	})
	t.Run("scope columns", func(t *testing.T) {
		define(t, &apiversions.Projection{Version: "v1", Exclude: []string{"tenant_id", "user_id", "deleted_at"}})
	})

	for name, p := range map[string]*apiversions.Projection{
		"injected-looking typo":    {Version: "v1", Exclude: []string{"updated"}},
		"timestamps off":           {Version: "v1", Exclude: []string{"created_at"}},
		"scope column not enabled": {Version: "v1", Include: []string{"tenant_id"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := entity.EntityConfig{Fields: base.Fields}
			if name == "timestamps off" {
				cfg = cfg.WithTimestamps(false)
			}
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("projection %+v naming a column the entity will not have was accepted", *p)
				}
			}()
			_ = apiversions.ApplyToEntityConfig(cfg, apiversions.NewProjectionSet(p), "v1")
		})
	}
}
