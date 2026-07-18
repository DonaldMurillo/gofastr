package framework

import (
	"context"
	"fmt"
)

// App-level wiring for process-isolated third-party modules (#37). Kept in a
// processmodule_-named file (not app.go) so its coverage is attributed to the
// process-module subsystem's coverage bucket rather than the App spine's — the
// wiring's defensive misconfiguration guards and the composite MCP call-gate
// closure share the subprocess subsystem's testability profile, not the
// spine's. See scripts/coverage-floors.sh.

// RegisterProcessModule installs a process-isolated third-party module
// (issue #37, wave 2a). It validates the descriptor, ensures the
// [ProcessModuleStore] schema, registers the module with the supervisor
// AND the in-process [ModuleManager] (so the route gate 404s when the
// module is disabled), mounts each declared route behind the gate, and
// wires the supervisor as the process-module coordinator for
// [ModuleManager.List] introspection.
//
// Routes are attributed to the module name via setCurrent/clearCurrent so
// the existing route gate (Enabled → 404, indistinguishable from
// uninstalled) governs them. The Ready layer lives in the proxy handler
// (enabled-but-not-Ready → 503 + Retry-After). The supervisor's loops
// are started in [App.runStartHooks]; the drain drainer is registered
// via [lifecycle.PrependDrainer] so children drain first during shutdown.
//
// Panics on validation or schema errors (caller-time misconfiguration —
// the descriptor is operator-supplied at install). Returns the App for
// chaining.
func (a *App) RegisterProcessModule(desc ProcessModuleDescriptor, approved ApprovedGrants) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q) called after InitPlugins; register before App.Start", desc.Name))
	}
	// Lazily construct the store + supervisor on first call.
	if a.processModules == nil {
		if a.DB == nil {
			panic("framework: RegisterProcessModule requires WithDB — the ProcessModuleStore is SQL-backed (design §8)")
		}
		store, err := NewSQLProcessModuleStore(a.DB)
		if err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule: %v", err))
		}
		if err := store.EnsureSchema(context.Background()); err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule ensure schema: %v", err))
		}
		// Construct the real capability broker (design #37 §5) and inject it
		broker := NewBroker(a.router, a.registryView(), a.Events(), a.apiPrefix())
		// The MCP tool registry (design §5.1): registers each module's
		// verified tools into a.MCP under `module.<name>.<tool>` and
		// answers the composite call gate so a disabled module's tools
		// are omitted from tools/list. Constructed before the supervisor
		// so it can be injected as the supervisor's ToolRegistrar.
		tools := NewModuleToolRegistry(a.MCP, nil) // sup wired below
		sup, err := NewProcessModuleSupervisor(SupervisorConfig{
			Store:         store,
			Broker:        broker,
			ToolRegistrar: tools,
		})
		if err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule supervisor: %v", err))
		}
		// Late-bind the supervisor into the registry (couldn't pass it to
		// NewModuleToolRegistry because it didn't exist yet). The
		// registry's handlers use it to look up the live peer + mint
		// delegations at call time.
		tools.sup = sup
		a.processModules = sup
		a.moduleTools = tools
		// Composite MCP call gate: module-namespaced tools route to the
		// registry (disabled → omitted+refused; enabled → listed, the
		// handler enforces Ready), everything else stays on the existing
		// in-process toolAllowed gate. Installed once, when the first
		// process module is registered (before InitPlugins, so it is not
		// overwritten by the default gate set during option application).
		inProcessToolGate := a.modules.toolAllowed
		a.MCP.SetCallGate(func(toolName string) error {
			if mod, ok := tools.ModuleFor(toolName); ok {
				return tools.GateForModule(mod)
			}
			return inProcessToolGate(toolName)
		})
		// Wire the supervisor as the coordinator so [ModuleManager.List]
		// populates operator introspection and [handleRemoteToggle]
		// forwards reconcile signals.
		a.modules.SetProcessCoordinator(processCoordinatorAdapter{sup: sup})
	}
	ctx := context.Background()
	if _, err := a.processModules.Register(ctx, desc, approved); err != nil {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q): %v", desc.Name, err))
	}
	// Register with the in-process manager so the route gate knows the
	// name. Manifest carries the migration group pointer; DependsOn stays
	// empty (process modules are enable-gated only this wave — design §2).
	a.modules.register(desc.Name, ModuleManifest{
		Version:        desc.Version,
		Description:    "process module: " + desc.Name,
		MigrationGroup: desc.MigrationGroup,
	})
	// Mount each declared route, attributed to the module name so the
	// route gate 404s when disabled. setCurrent/clearCurrent bracket the
	// registration so the router's register hook stamps the right owner.
	for _, route := range desc.Routes {
		a.modules.setCurrent(desc.Name)
		a.router.Handle(route.Method, route.Path, a.processModules.ProxyHandler(desc.Name, route.ID))
		a.modules.clearCurrent()
	}
	// Process modules default to DISABLED (InstalledDisabled) — the
	// operator enables via [App.ProcessModules].Enable after install.
	a.modules.mu.Lock()
	a.modules.enabled[desc.Name] = false
	a.modules.mu.Unlock()
	return a
}

// ProcessModules returns the process-module supervisor, or nil when the
// app registered no process modules. Through it the operator enables /
// disables / revokes a module at runtime.
func (a *App) ProcessModules() *ProcessModuleSupervisor {
	return a.processModules
}

// NewModuleMigrationCoordinator builds the short-lived migration
// coordinator for a process module (design §7). The operator (or a CLI /
// deploy job) calls it, runs [MigrationCoordinator.Apply] with the
// digest-verified migration set, and the coordinator provisions the
// per-module Postgres schema+role, runs Up authenticated as that role,
// and stamps MigrationsAppliedAt so the supervisor lets the module reach
// Ready. Returns an error if no process modules are registered.
//
// For the Postgres path, pass [WithCoordinatorAdminDSN] with the host's
// URL-form DSN (the same one the app boots against). SQLite is
// trusted/dev-only — the coordinator rejects an untrusted module's
// migrations on SQLite (fail-closed, design §7 decision F).
func (a *App) NewModuleMigrationCoordinator(opts ...CoordinatorOption) (*MigrationCoordinator, error) {
	if a.processModules == nil {
		return nil, fmt.Errorf("framework: no process modules registered")
	}
	if a.DB == nil {
		return nil, fmt.Errorf("framework: migration coordinator requires WithDB")
	}
	return NewMigrationCoordinator(a.processModules.store, a.DB, opts...)
}

// processCoordinatorAdapter bridges [ProcessModuleSupervisor] (which has
// Reconcile + Info(name)) to [processModuleCoordinator] (which wants
// Info(name) (ProcessModuleInfo, bool)). The adapter returns ok=false for
// any name the supervisor does not supervise.
type processCoordinatorAdapter struct{ sup *ProcessModuleSupervisor }

// Reconcile forwards to the supervisor.
func (a processCoordinatorAdapter) Reconcile(name string) { a.sup.Reconcile(name) }

// Info returns the supervisor's introspection, or ok=false when the name
// is not a registered process module.
func (a processCoordinatorAdapter) Info(name string) (ProcessModuleInfo, bool) {
	info, err := a.sup.Info(name)
	if err != nil {
		return ProcessModuleInfo{}, false
	}
	return info, true
}
