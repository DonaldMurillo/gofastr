package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// buildTogglePayload constructs a fanout envelope for a module toggle,
// matching the format ModuleManager.publish uses.
func buildTogglePayload(nodeID, name string, enabled bool) []byte {
	body, _ := json.Marshal(moduleToggleMessage{Name: name, Enabled: enabled})
	return fanout.Wrap(nodeID, body)
}

// Test cronGate closure invocation
func TestCronGateClosureInvoked(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	app.InitPlugins()

	gate := app.Modules().cronGate("m1")
	if !gate("any") {
		t.Fatal("cronGate should return true for enabled module")
	}
	app.Modules().Disable(context.Background(), "m1")
	if gate("any") {
		t.Fatal("cronGate should return false for disabled module")
	}
}

// Test RegisterModule panic after initialization
func TestRegisterModuleAfterInit(t *testing.T) {
	app := NewApp()
	app.InitPlugins()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic registering module after init")
		}
	}()
	app.RegisterModule(&modStub{name: "late", manifest: ModuleManifest{}, init: noopInit})
}

// Test recordEntity during module Init (entity attribution)
func TestRecordEntityDuringModuleInit(t *testing.T) {
	app := NewApp()
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{},
		init: func(app *App) error {
			// Entity without DB just registers in the registry.
			// Use TryEntity to avoid panic on misconfig.
			_ = app.TryEntity("widgets", minimalEntityConfig())
			return nil
		},
	})
	app.InitPlugins()

	list := app.Modules().List()
	if len(list) != 1 {
		t.Fatalf("expected 1 module, got %d", len(list))
	}
	if list[0].EntityCount != 1 {
		t.Fatalf("expected 1 entity, got %d", list[0].EntityCount)
	}
}

// Test recordRoute/recordTool/recordEntity when no module is current
func TestAttributionNoCurrentModule(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.recordRoute("GET", "/test")
	mm.recordTool("test_tool")
	mm.recordEntity("test_entity")
	// Nothing should be attributed.
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	if len(mm.routes) != 0 || len(mm.tools) != 0 || len(mm.entities) != 0 {
		t.Fatal("attribution should not happen when no module is current")
	}
}

// Test List with entities and tools counted
func TestListWithEntityAndToolCounts(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	app.RegisterModule(&modStub{
		name:     "m1",
		manifest: ModuleManifest{Version: "v1", MigrationGroup: "custom"},
		init: func(app *App) error {
			app.Router().Get("/a", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			app.Router().Get("/b", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			return app.MCP.RegisterTool("t1", "d", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
				return nil, nil
			})
		},
	})
	app.InitPlugins()

	list := app.Modules().List()
	if len(list) != 1 {
		t.Fatalf("expected 1 module, got %d", len(list))
	}
	m := list[0]
	if m.RouteCount != 2 {
		t.Fatalf("expected 2 routes, got %d", m.RouteCount)
	}
	if m.ToolCount != 1 {
		t.Fatalf("expected 1 tool, got %d", m.ToolCount)
	}
	if m.MigrationGroup != "custom" {
		t.Fatalf("expected migration group 'custom', got %q", m.MigrationGroup)
	}
}

// Test HandleRemoteToggle: refreshes from store, ignores unknown names.
func TestHandleRemoteToggleRefreshFromStore(t *testing.T) {
	mm1 := NewModuleManager(nil, nil)
	mm2 := NewModuleManager(nil, nil)
	// Share the store so mm2 reads what mm1 persisted.
	mm2.store = mm1.store
	mm1.register("m1", ModuleManifest{})
	mm2.register("m1", ModuleManifest{})
	mm1.loadFromStore(context.Background())
	mm2.loadFromStore(context.Background())

	// mm1 disables m1 (writes to the shared store).
	mm1.Disable(context.Background(), "m1")

	// mm2 receives the remote toggle — should refresh from the store.
	payload := buildTogglePayload(mm1.nodeID, "m1", false)
	mm2.handleRemoteToggle(payload)
	if mm2.Enabled("m1") {
		t.Fatal("mm2 should see m1 as disabled after refresh from store")
	}
}

// Test HandleRemoteToggle: ignores unregistered module names.
func TestHandleRemoteToggleIgnoresUnknown(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())

	// Crafted payload for an unregistered module — must not pollute cache.
	payload := buildTogglePayload("other-node", "bogus", false)
	mm.handleRemoteToggle(payload)
	// "bogus" is not registered; cache should be unchanged.
	if !mm.Enabled("bogus") {
		t.Fatal("unregistered name should not affect cache (still enabled by default)")
	}
}

// Test HandleRemoteToggle: crafted enable payload cannot override store.
func TestHandleRemoteToggleCraftedPayloadIgnored(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())

	// Store says disabled.
	mm.store.SetEnabled(context.Background(), "m1", false)
	mm.mu.Lock()
	mm.enabled["m1"] = false
	mm.mu.Unlock()

	// Crafted payload claims enabled — must NOT override the store.
	payload := buildTogglePayload("other-node", "m1", true)
	mm.handleRemoteToggle(payload)
	if mm.Enabled("m1") {
		t.Fatal("crafted enable payload must not override store's disabled state")
	}
}

func minimalEntityConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table:  "widgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false)
}
