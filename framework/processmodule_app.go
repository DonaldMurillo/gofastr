package framework

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// RegisterProcessModule validates desc, registers it with the process-module
// supervisor and the module manager, and mounts its routes behind the module
// route gate. The module starts disabled. It panics on an invalid descriptor, a
// missing DB, a refused sandbox or a call after InitPlugins.
func (a *App) RegisterProcessModule(desc ProcessModuleDescriptor, approved ApprovedGrants) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q) called after InitPlugins; register before App.Start", desc.Name))
	}
	if a.processModules == nil {
		if a.DB == nil {
			panic("framework: RegisterProcessModule requires WithDB: the ProcessModuleStore is SQL-backed (design §8)")
		}
		store, err := NewSQLProcessModuleStore(a.DB)
		if err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule: %v", err))
		}
		if err := store.EnsureSchema(context.Background()); err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule ensure schema: %v", err))
		}
		broker := NewBroker(a.router, a.registryView(), a.Events(), a.apiPrefix())
		tools := NewModuleToolRegistry(a.MCP, nil)
		sup, err := NewProcessModuleSupervisor(SupervisorConfig{
			Store:         store,
			Broker:        broker,
			ToolRegistrar: tools,
			Logf: func(format string, args ...any) {
				a.Logger().Warn(textsafe.ScrubControlBytes(fmt.Sprintf(format, args...)))
			},
		})
		if err != nil {
			panic(fmt.Sprintf("framework: RegisterProcessModule supervisor: %v", err))
		}
		tools.sup = sup
		a.processModules = sup
		a.moduleTools = tools
		inProcessToolGate := a.modules.toolAllowed
		a.MCP.SetCallGate(func(toolName string) error {
			if mod, ok := tools.ModuleFor(toolName); ok {
				return tools.GateForModule(mod)
			}
			return inProcessToolGate(toolName)
		})
		a.modules.SetProcessCoordinator(processCoordinatorAdapter{sup: sup})
	}
	if desc.TrustTier == TrustUntrusted {
		a.installSandboxFor(desc, approved)
	}
	ctx := context.Background()
	reg, err := a.processModules.Register(ctx, desc, approved)
	if err != nil {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q): %v", desc.Name, err))
	}
	if reg.Outcome == InstallGrantsChanged {
		a.Logger().Warn("process module grants changed; disabled until an operator re-enables it",
			"module", desc.Name,
			"previous_grants", grantsAsStrings(reg.PreviousGrants),
			"grants", grantsAsStrings(reg.EffectiveGrants))
	}
	a.modules.register(desc.Name, ModuleManifest{
		Version:        desc.Version,
		Description:    "process module: " + desc.Name,
		MigrationGroup: desc.MigrationGroup,
	})
	for _, route := range desc.Routes {
		a.modules.setCurrent(desc.Name)
		a.router.Handle(route.Method, route.Path, a.processModules.ProxyHandler(desc.Name, route.ID))
		a.modules.clearCurrent()
	}
	a.modules.markProcess(desc.Name)
	return a
}

func (a *App) installSandboxFor(desc ProcessModuleDescriptor, approved ApprovedGrants) {
	if _, err := ValidateProcessModuleDescriptor(desc, approved); err != nil {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q): %v", desc.Name, err))
	}
	runner, err := a.ProcessModuleSandbox()
	if err != nil {
		panic(fmt.Sprintf("framework: RegisterProcessModule(%q): %v", desc.Name, &UntrustedNoSandboxError{Module: desc.Name, cause: err}))
	}
	a.processModules.setSandbox(runner)
}

// ProcessModules returns the process-module supervisor, or nil when none is registered.
func (a *App) ProcessModules() *ProcessModuleSupervisor {
	return a.processModules
}

// NewModuleMigrationCoordinator returns a coordinator that applies a process
// module's migrations; it errors when no process module or DB is registered.
// SQLite refuses an untrusted module's migrations.
func (a *App) NewModuleMigrationCoordinator(opts ...CoordinatorOption) (*MigrationCoordinator, error) {
	if a.processModules == nil {
		return nil, fmt.Errorf("framework: no process modules registered")
	}
	if a.DB == nil {
		return nil, fmt.Errorf("framework: migration coordinator requires WithDB")
	}
	return NewMigrationCoordinator(a.processModules.store, a.DB, opts...)
}

type processCoordinatorAdapter struct{ sup *ProcessModuleSupervisor }

func (a processCoordinatorAdapter) Reconcile(name string) { a.sup.Reconcile(name) }

func (a processCoordinatorAdapter) DesiredEnabled(name string) bool {
	sl := a.sup.Slot(name)
	return sl != nil && sl.snapshot().enabled
}

func (a processCoordinatorAdapter) SetDesiredEnabled(ctx context.Context, name string, enabled bool) error {
	if enabled {
		return a.sup.Enable(ctx, name)
	}
	return a.sup.Disable(ctx, name)
}

func (a processCoordinatorAdapter) Info(name string) (ProcessModuleInfo, bool) {
	info, err := a.sup.Info(name)
	if err != nil {
		return ProcessModuleInfo{}, false
	}
	return info, true
}

func (a processCoordinatorAdapter) list(ctx context.Context) []ProcessModuleInfo {
	return a.sup.list(ctx)
}
