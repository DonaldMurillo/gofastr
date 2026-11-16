package framework

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Role selects which responsibilities a process assumes at boot.
// WithRole wins over the GOFASTR_ROLE env var, which wins over RoleAll;
// an unknown value in either fails NewApp.
type Role string

const (
	// RoleAll serves HTTP and runs background consumers (cron, queues,
	// the outbox relay). The default.
	RoleAll Role = "all"

	// RoleServe serves the full HTTP surface and starts no worker-scoped
	// consumers. Plain OnStart hooks still run; gate them on App.Role.
	RoleServe Role = "serve"

	// RoleWorker runs background consumers and serves only /healthz and
	// /readyz.
	RoleWorker Role = "worker"

	// RoleAgent serves only /mcp, the A2A path and the health endpoints,
	// through the app router's middleware, and starts no worker-scoped
	// consumers.
	RoleAgent Role = "agent"
)

func isValidRole(r Role) bool {
	switch r {
	case RoleAll, RoleServe, RoleWorker, RoleAgent:
		return true
	}
	return false
}

// WithRole sets the process role, overriding GOFASTR_ROLE. NewApp panics
// on an unknown role.
func WithRole(r Role) AppOption {
	return func(a *App) {
		a.roleOpt = r
		a.roleSet = true
	}
}

func resolveRole(opt Role, optSet bool) (Role, error) {
	if optSet {
		if !isValidRole(opt) {
			return "", fmt.Errorf("framework: invalid role %q (want all, serve, worker, or agent)", opt)
		}
		return opt, nil
	}
	if v := os.Getenv("GOFASTR_ROLE"); v != "" {
		switch r := Role(strings.ToLower(v)); r {
		case RoleAll, RoleServe, RoleWorker, RoleAgent:
			return r, nil
		default:
			return "", fmt.Errorf("framework: invalid GOFASTR_ROLE %q (want all, serve, worker, or agent)", v)
		}
	}
	return RoleAll, nil
}

// Role returns the process role NewApp resolved.
func (a *App) Role() Role { return a.role }

func (a *App) runsWorkers() bool {
	return a.role == RoleAll || a.role == RoleWorker
}

func (a *App) roleHandler() http.Handler {
	if a.role == RoleWorker {
		return a.workerHealthMux()
	}
	if a.role == RoleAgent {
		return a.agentMux()
	}
	return a.router
}

func (a *App) workerHealthMux() http.Handler {
	liveness, readiness := a.healthHandlers()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", readiness)
	return mux
}

func (a *App) agentMux() http.Handler {
	liveness, readiness := a.healthHandlers()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", readiness)
	mux.Handle("/mcp", a.router)
	mux.Handle("/mcp/", a.router)
	if a.a2aPath != "" {
		mux.Handle(a.a2aPath, a.router)
		mux.Handle(a.a2aPath+"/", a.router)
	}
	return mux
}
