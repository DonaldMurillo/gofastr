package framework

import (
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

func TestDefineEntityWithFields(t *testing.T) {
	e := Define("users", EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: ptrFloat(200)},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
			{Name: "age", Type: schema.Int, Min: ptrFloat(0), Max: ptrFloat(150)},
		},
	})

	if e.GetName() != "users" {
		t.Errorf("expected name %q, got %q", "users", e.GetName())
	}
	if e.GetTable() != "users" {
		t.Errorf("expected table %q, got %q", "users", e.GetTable())
	}
	if len(e.GetFields()) != 6 {
		t.Fatalf("expected 6 fields (3 user + id + created_at + updated_at), got %d", len(e.GetFields()))
	}
	if e.GetFields()[0].Name != "id" {
		t.Errorf("expected first field %q, got %q", "id", e.GetFields()[0].Name)
	}

	// Timestamps should default to true
	if !e.Config.Timestamps {
		t.Error("expected Timestamps to default to true")
	}
}

func TestDefineEntityWithExplicitTable(t *testing.T) {
	e := Define("User", EntityConfig{
		Table: "app_users",
		Fields: []schema.Field{
		},
	})

	if e.GetTable() != "app_users" {
		t.Errorf("expected table %q, got %q", "app_users", e.GetTable())
	}
}

func TestDefineEntityTimestampsOptOut(t *testing.T) {
	e := Define("logs", EntityConfig{
		Fields: []schema.Field{
			{Name: "message", Type: schema.Text},
		},
	}.WithTimestamps(false))

	if e.Config.Timestamps {
		t.Error("expected Timestamps to be false when explicitly disabled")
	}
}

func TestRegisterEntityInRegistry(t *testing.T) {
	reg := NewRegistry()

	e := Define("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})

	if err := reg.Register(e); err != nil {
		t.Fatalf("failed to register entity: %v", err)
	}
}

func TestGetEntityFromRegistry(t *testing.T) {
	reg := NewRegistry()

	e := Define("comments", EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
		},
	})
	reg.Register(e)

	got, err := reg.Get("comments")
	if err != nil {
		t.Fatalf("failed to get entity: %v", err)
	}
	if got.GetName() != "comments" {
		t.Errorf("expected name %q, got %q", "comments", got.GetName())
	}
}

func TestGetEntityNotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing entity, got nil")
	}
}

func TestDuplicateNameReturnsError(t *testing.T) {
	reg := NewRegistry()

	e1 := Define("tags", EntityConfig{
	})
	e2 := Define("tags", EntityConfig{
	})

	if err := reg.Register(e1); err != nil {
		t.Fatalf("first register should succeed: %v", err)
	}
	if err := reg.Register(e2); err == nil {
		t.Error("expected error for duplicate name, got nil")
	}
}

func TestRegistryAll(t *testing.T) {
	reg := NewRegistry()

	reg.Register(Define("users", EntityConfig{
	}))
	reg.Register(Define("posts", EntityConfig{
	}))

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2 entities, got %d", len(all))
	}
	if _, ok := all["users"]; !ok {
		t.Error("expected 'users' in All()")
	}
	if _, ok := all["posts"]; !ok {
		t.Error("expected 'posts' in All()")
	}
}

func TestAppFluentAPI(t *testing.T) {
	app := NewApp()

	result := app.Entity("articles", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "author", Type: schema.Relation, To: "users"},
		},
		CRUD: true,
		MCP:  true,
	})

	// Fluent: Entity returns *App
	if result != app {
		t.Error("Entity() should return the same *App for chaining")
	}

	// Verify entity was registered
	e, err := app.Registry.Get("articles")
	if err != nil {
		t.Fatalf("failed to get registered entity: %v", err)
	}
	if e.GetName() != "articles" {
		t.Errorf("expected name %q, got %q", "articles", e.GetName())
	}
	if len(e.GetFields()) != 6 {
		t.Errorf("expected 6 fields, got %d", len(e.GetFields()))
	}
	if !e.Config.CRUD {
		t.Error("expected CRUD to be true")
	}
	if !e.Config.MCP {
		t.Error("expected MCP to be true")
	}
}

func TestAppFluentChaining(t *testing.T) {
	app := NewApp()

	app.Entity("users", EntityConfig{
	}).Entity("posts", EntityConfig{
	}).Entity("comments", EntityConfig{
	})

	all := app.Registry.All()
	if len(all) != 3 {
		t.Errorf("expected 3 entities after fluent chaining, got %d", len(all))
	}
}

func TestAppWithDB(t *testing.T) {
	app := NewApp()

	// Should not panic when no DB is set
	app.Entity("items", EntityConfig{
	})

	e, _ := app.Registry.Get("items")
	if e.DB != nil {
		t.Error("expected DB to be nil when not set on app")
	}
}

func TestEntityValidation(t *testing.T) {
	tests := []struct {
		name    string
		entity  *Entity
		wantErr bool
	}{
		{
			name: "valid entity",
			entity: Define("valid", EntityConfig{
				Fields: []schema.Field{
					{Name: "name", Type: schema.String},
				},
			}),
			wantErr: false,
		},
		{
			name: "empty name",
			entity: &Entity{
				Config: EntityConfig{
				},
			},
			wantErr: true,
		},
		{
			name: "no fields",
			entity: Define("empty", EntityConfig{
				Fields: []schema.Field{},
			}),
			wantErr: false, // auto-injected id + timestamps count as fields
		},
		{
			name: "duplicate field name",
			entity: Define("dup", EntityConfig{
				Fields: []schema.Field{
					{Name: "id", Type: schema.Int},
				},
			}),
			wantErr: false, // explicit id field overrides auto-injected one, no duplicate
		},
		{
			name: "relation without target",
			entity: Define("badrel", EntityConfig{
				Fields: []schema.Field{
					{Name: "author", Type: schema.Relation},
				},
			}),
			wantErr: true,
		},
		{
			name: "relation with target",
			entity: Define("goodrel", EntityConfig{
				Fields: []schema.Field{
					{Name: "author", Type: schema.Relation, To: "users"},
				},
			}),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.entity.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ptrFloat is a test helper to create a *float64 from a literal.
func ptrFloat(v float64) *float64 {
	return &v
}
