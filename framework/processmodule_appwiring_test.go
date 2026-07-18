package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// TestApp_RegisterProcessModuleWiring covers the App-level process-module
// wiring on the framework spine: RegisterProcessModule (first-call lazy
// construction of the store + broker + supervisor + tool registry + coordinator
// adapter), ProcessModules, and NewModuleMigrationCoordinator. It stops at
// registration — spawning a child is exercised by the supervisor/gate suites;
// here the point is that the App entry points construct and wire correctly.
func TestApp_RegisterProcessModuleWiring(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })

	app := NewApp(WithDB(db))

	desc := validDescriptor()
	app.RegisterProcessModule(desc, ApprovedGrants{"articles:read", "articles:write"})

	sup := app.ProcessModules()
	if sup == nil {
		t.Fatal("ProcessModules() returned nil after RegisterProcessModule")
	}
	if _, err := sup.Info(desc.Name); err != nil {
		t.Fatalf("registered module %q not found in supervisor: %v", desc.Name, err)
	}
	if got := sup.List(); len(got) != 1 || got[0].Name != desc.Name {
		t.Fatalf("List() = %+v, want one entry named %q", got, desc.Name)
	}

	// A second module reuses the already-constructed supervisor (the non-first
	// path through RegisterProcessModule). Distinct route paths so they don't
	// collide on the shared router.
	second := validDescriptor()
	second.Name = "demo2"
	second.Routes = []RouteDeclaration{
		{ID: "list", Method: "GET", Path: "/demo2/items"},
		{ID: "get", Method: "GET", Path: "/demo2/items/:id"},
	}
	app.RegisterProcessModule(second, ApprovedGrants{"articles:read"})
	if len(app.ProcessModules().List()) != 2 {
		t.Fatalf("second register: List() len = %d, want 2", len(app.ProcessModules().List()))
	}

	// The migration coordinator constructs off the live supervisor's store.
	coord, err := app.NewModuleMigrationCoordinator()
	if err != nil {
		t.Fatalf("NewModuleMigrationCoordinator: %v", err)
	}
	if coord == nil {
		t.Fatal("NewModuleMigrationCoordinator returned nil coordinator")
	}

	// The in-process ModuleManager's List merges the supervisor's operator
	// introspection (design §8) via the coordinator adapter — a registered
	// process module appears with its ProcessState populated.
	var seen bool
	for _, mi := range app.modules.List() {
		if mi.Name == desc.Name {
			seen = true
			if mi.ProcessState == "" {
				t.Errorf("module %q listed with empty ProcessState — coordinator introspection not wired", mi.Name)
			}
		}
	}
	if !seen {
		t.Fatalf("registered process module %q not in ModuleManager.List()", desc.Name)
	}
}

// TestApp_RegisterProcessModuleInvalidPanics covers the register-error guard:
// a descriptor the supervisor rejects (a non-grantable capability) panics with
// a clear message rather than registering a broken module.
func TestApp_RegisterProcessModuleInvalidPanics(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db))

	bad := validDescriptor()
	bad.Name = "bad"
	bad.RequestedGrants = []access.Permission{"CrossOwnerRead"} // non-grantable (design §5)

	defer func() {
		if recover() == nil {
			t.Fatal("RegisterProcessModule with a non-grantable capability: want panic, got none")
		}
	}()
	app.RegisterProcessModule(bad, ApprovedGrants{"CrossOwnerRead"})
}

// TestApp_CoordinatorWithoutModules covers the guard: asking for a migration
// coordinator before any process module is registered is an error, not a panic.
func TestApp_CoordinatorWithoutModules(t *testing.T) {
	app := NewApp()
	if _, err := app.NewModuleMigrationCoordinator(); err == nil {
		t.Fatal("NewModuleMigrationCoordinator with no modules: want error, got nil")
	}
}

// TestApp_RegisterProcessModuleNoDBPanics covers the WithDB requirement guard.
func TestApp_RegisterProcessModuleNoDBPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterProcessModule without WithDB: want panic, got none")
		}
	}()
	NewApp().RegisterProcessModule(validDescriptor(), ApprovedGrants{access.Permission("articles:read")})
}

// TestApp_RegisterProcessModuleAfterInitPanics covers the register-before-Start
// contract guard: a late registration (after InitPlugins) panics rather than
// racing the running supervisor.
func TestApp_RegisterProcessModuleAfterInitPanics(t *testing.T) {
	db := newStoreDB(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db))
	app.initialized.Store(true)
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterProcessModule after init: want panic, got none")
		}
	}()
	app.RegisterProcessModule(validDescriptor(), ApprovedGrants{"articles:read"})
}

// recordingCoordinator is a processModuleCoordinator fake that records the
// names passed to Reconcile, for the remote-toggle forwarding test.
type recordingCoordinator struct{ reconciled []string }

func (c *recordingCoordinator) Reconcile(name string) { c.reconciled = append(c.reconciled, name) }
func (c *recordingCoordinator) Info(string) (ProcessModuleInfo, bool) {
	return ProcessModuleInfo{}, false
}

// TestModuleManager_RemoteToggleForwardsReconcile covers the process-module
// reconcile hook in handleRemoteToggle (design §8, the remote-toggle reconcile
// source): a toggle from another replica, after refreshing the in-process
// cache, fans a reconcile nudge into the process supervisor.
func TestModuleManager_RemoteToggleForwardsReconcile(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())
	coord := &recordingCoordinator{}
	mm.SetProcessCoordinator(coord)

	// A toggle re-published by THIS node is ignored (no reconcile) — the
	// own-publish guard prevents a self-triggered reconcile loop.
	mm.handleRemoteToggle(buildTogglePayload(mm.nodeID, "m1", false))
	if len(coord.reconciled) != 0 {
		t.Fatalf("own-node toggle triggered reconcile %v, want none", coord.reconciled)
	}

	// A toggle from another replica forwards a reconcile nudge.
	payload := buildTogglePayload("other-node", "m1", false)
	mm.handleRemoteToggle(payload)

	if len(coord.reconciled) != 1 || coord.reconciled[0] != "m1" {
		t.Fatalf("remote toggle: coordinator.Reconcile calls = %v, want [m1]", coord.reconciled)
	}

	// List with a coordinator attached but Info reporting ok=false (m1 is an
	// in-process module, not one this supervisor runs) must leave the
	// process-only fields zero — the additive-introspection contract.
	for _, mi := range mm.List() {
		if mi.Name == "m1" && mi.ProcessState != "" {
			t.Errorf("in-process module m1 got a ProcessState %q from a non-owning coordinator", mi.ProcessState)
		}
	}
}

// TestModuleManager_RemoteToggleStoreErrorFailsOpen covers the fanout-refresh
// fail-open branch: if the authoritative store can't be re-read when a remote
// toggle arrives, the manager keeps its current cache (logs a WARN) and does
// NOT forward a reconcile — an unreadable store must not flap the cache off a
// lossy signal.
func TestModuleManager_RemoteToggleStoreErrorFailsOpen(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())
	mm.store = loadFailingStore{} // Load errors on the fanout refresh
	coord := &recordingCoordinator{}
	mm.SetProcessCoordinator(coord)

	mm.handleRemoteToggle(buildTogglePayload("other-node", "m1", false))

	if !mm.Enabled("m1") {
		t.Error("store-load failure must keep the current cache (m1 stayed enabled by default)")
	}
	if len(coord.reconciled) != 0 {
		t.Errorf("reconcile forwarded despite store-load failure: %v", coord.reconciled)
	}
}

// TestModuleManager_RemoteToggleMalformedBody covers the guard against a
// well-framed fanout envelope carrying an unparseable toggle body: it is
// dropped (no cache change, no reconcile), not treated as a toggle.
func TestModuleManager_RemoteToggleMalformedBody(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("m1", ModuleManifest{})
	mm.loadFromStore(context.Background())
	coord := &recordingCoordinator{}
	mm.SetProcessCoordinator(coord)

	// Valid fanout envelope from another node, but the inner body is not a
	// toggle message.
	mm.handleRemoteToggle(fanout.Wrap("other-node", []byte("{not-a-toggle")))

	if len(coord.reconciled) != 0 {
		t.Errorf("malformed toggle body triggered reconcile: %v", coord.reconciled)
	}
}
