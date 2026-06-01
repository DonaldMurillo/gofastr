package framework

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// WithMCPServer installs a caller-supplied MCP server.
func TestCovWithMCPServer(t *testing.T) {
	srv := mcp.NewServer()
	app := NewApp(WithMCPServer(srv), WithoutDefaultMiddleware())
	if app.MCP != srv {
		t.Fatal("WithMCPServer did not install the server")
	}
}

// Entity panics on a duplicate name (Registry.Register error, line 571).
func TestCovEntityDuplicatePanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("dup", entity.EntityConfig{
		Fields: []schema.Field{{Name: "a", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate-entity panic")
		}
	}()
	app.Entity("dup", entity.EntityConfig{
		Fields: []schema.Field{{Name: "b", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
}

// GroupEntity panics on a duplicate name (line 375).
func TestCovGroupEntityDuplicatePanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api")
	app.GroupEntity(g, "dupg", entity.EntityConfig{
		Fields: []schema.Field{{Name: "a", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate group-entity panic")
		}
	}()
	app.GroupEntity(g, "dupg", entity.EntityConfig{
		Fields: []schema.Field{{Name: "b", Type: schema.String}},
		CRUD:   boolPtr(false),
	})
}

// View panics on a duplicate-name view entity (line 684). Register an entity,
// then a view whose ToEntity() collides on name.
func TestCovViewDuplicatePanics(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("things", entity.EntityConfig{
			Table:  "things",
			Fields: []schema.Field{{Name: "id", Type: schema.String}},
		}.WithTimestamps(false))
		defer func() {
			if recover() == nil {
				t.Fatal("expected duplicate-view panic")
			}
		}()
		// A view named "things" collides with the entity above.
		app.View(migrate.View{
			Name:   "things",
			Select: "SELECT id FROM things",
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true},
			},
		})
	})
}

// registerEntityEndpoints surfaces the MCP RegisterTool error (line 801) when
// two endpoints declare the same tool Name.
func TestCovEntityEndpointDuplicateToolPanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic from duplicate MCP tool name")
		}
	}()
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
		Endpoints: []entity.Endpoint{
			{
				Method: http.MethodPost, Path: "a", Name: "dup_tool", MCP: true,
				Handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
			},
			{
				Method: http.MethodPost, Path: "b", Name: "dup_tool", MCP: true,
				Handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
			},
		},
	})
}

// registerGroupEndpoints surfaces the MCP RegisterTool error (line 442).
func TestCovGroupEndpointDuplicateToolPanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/api", routegroup.WithMCPNamespace("ns"))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic from duplicate group MCP tool name")
		}
	}()
	app.GroupEntity(g, "posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		CRUD:   boolPtr(false),
		Endpoints: []entity.Endpoint{
			{
				Method: http.MethodPost, Path: "a", Name: "dup", MCP: true,
				Handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
			},
			{
				Method: http.MethodPost, Path: "b", Name: "dup", MCP: true,
				Handler:    http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
				MCPHandler: func(context.Context, map[string]any) (any, error) { return nil, nil },
			},
		},
	})
}

// RegisterBattery panics if called after InitPlugins (line 852).
func TestCovRegisterBatteryAfterInitPanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic: RegisterBattery after InitPlugins")
		}
	}()
	app.RegisterBattery(&mockBattery{name: "late"})
}

// InitPlugins rolls back the latch and returns the error when a battery
// fails Init (app.go line 882-885).
func TestCovInitPluginsBatteryError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterBattery(&mockBattery{name: "failer", initErr: errStored})
	if err := app.InitPlugins(); err == nil {
		t.Fatal("expected InitPlugins to fail on battery init error")
	}
	// Latch rolled back → a retry is possible (initialized flag cleared).
	if app.initialized.Load() {
		t.Fatal("initialized latch should have rolled back after battery failure")
	}
}

// registerIntrospectionTools surfaces a RegisterTool error (mcp line 108)
// and InitPlugins propagates it (app.go line 894) when an introspection
// tool name is already taken.
func TestCovIntrospectionRegisterError(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	// Pre-register a tool whose name collides with an introspection tool.
	if err := app.MCP.RegisterTool("app_routes", "x", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := app.InitPlugins(); err == nil {
		t.Fatal("expected InitPlugins to fail from duplicate introspection tool name")
	}
}

// Start fails when InitPlugins fails during boot (app.go line 1121).
func TestCovStartInitPluginsError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterPlugin(covFailPlugin{})
	if err := app.Start("127.0.0.1:0"); err == nil {
		t.Fatal("expected Start to fail from a failing plugin Init")
	}
}

// Start fails when ListenAndServe can't bind (app.go line 1220). A
// malformed address fails the bind without touching migrations.
func TestCovStartListenError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if err := app.Start("256.256.256.256:99999"); err == nil {
		t.Fatal("expected Start to fail binding an invalid address")
	}
}

// toolBatteries skips a name whose entry is nil (mcp line 145). Force the
// inconsistent internal state directly.
func TestCovToolBatteriesNilEntry(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterBattery(&mockBattery{name: "real"})
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	// Inject a name that resolves to a nil entry so the skip branch fires.
	app.Batteries.sorted = append(app.Batteries.sorted, "ghost")
	app.Batteries.entries["ghost"] = nil
	res, err := app.toolBatteries(context.Background(), nil)
	if err != nil {
		t.Fatalf("toolBatteries: %v", err)
	}
	m := res.(map[string]any)
	// Only the real battery is reported; ghost is skipped.
	if m["count"].(int) != 1 {
		t.Fatalf("expected ghost entry skipped, count=%v", m["count"])
	}
}

// unmarshalHookPayload surfaces the json.Marshal error in BOTH the map
// branch (line 172) and the non-map branch (line 180), and the decode
// error in the non-map branch (line 183).
func TestCovUnmarshalHookPayloadErrors(t *testing.T) {
	var dst struct {
		Field string `json:"field"`
	}
	// Map branch: a value that can't be marshalled (func).
	if err := unmarshalHookPayload(map[string]any{"x": func() {}}, &dst); err == nil {
		t.Fatal("expected map-branch marshal error")
	}
	// Non-map branch: a bare func can't be marshalled (line 180).
	if err := unmarshalHookPayload(func() {}, &dst); err == nil {
		t.Fatal("expected non-map marshal error")
	}
	// Non-map branch: a number marshals fine but can't decode into a struct.
	if err := unmarshalHookPayload(123, &dst); err == nil {
		t.Fatal("expected non-map decode error")
	}
}

// marshalMergeIntoMap surfaces the json.Unmarshal-into-map error when src
// marshals to a non-object (typed_hooks line 263).
func TestCovMarshalMergeUnmarshalError(t *testing.T) {
	if err := marshalMergeIntoMap(123, map[string]any{}); err == nil {
		t.Fatal("expected unmarshal-into-map error for non-object src")
	}
}

// OnBeforeUpdate propagates the callback error (typed_hooks line 71).
func TestCovOnBeforeUpdateError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeUpdate(app, "e", func(context.Context, *covTyped) error { return errStored })
	hr := app.HookRegistry("e")
	if err := hr.ExecuteHooks(context.Background(), hook.BeforeUpdate, map[string]any{"count": 1}); err == nil {
		t.Fatal("expected callback error to propagate")
	}
}
