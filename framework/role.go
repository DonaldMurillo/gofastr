package framework

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Role selects which responsibilities a single binary assumes at boot.
// One binary, role picked at deploy time — so background load (cron,
// queue workers, the outbox relay) can run in a dedicated process that
// doesn't share a listener with request serving.
//
// Resolution precedence (see resolveRole): WithRole > the GOFASTR_ROLE
// env var > RoleAll. An unknown value in either place fails loudly in
// NewApp — a typo'd role must never silently run the wrong workload.
type Role string

const (
	// RoleAll is the default: serve HTTP AND run background consumers
	// (cron, queues, the outbox relay). Exactly today's behavior — zero
	// change for existing apps.
	RoleAll Role = "all"

	// RoleServe runs the full HTTP surface (router, auto-migrate, seeds,
	// plugins, batteries) but does NOT start worker-scoped consumers:
	// AddCron/AddQueue registrations and the outbox relay are skipped, so
	// a serve-only process never starts — nor later tries to drain — a
	// scheduler it never owned. Plain OnStart hooks still run; gate your
	// own via App.Role().
	RoleServe Role = "serve"

	// RoleWorker runs background consumers (cron, queues, outbox relay)
	// and binds addr, but serves ONLY the health surface (/healthz +
	// /readyz). It does NOT mount the app router, entity CRUD, OpenAPI,
	// docs, admin, or well-known discovery routes. Auto-migrate, seeds,
	// plugins, and batteries still run (migrations take a lock; either
	// process type may boot first).
	RoleWorker Role = "worker"
)

// isValidRole reports whether r is one of the defined Role constants.
func isValidRole(r Role) bool {
	switch r {
	case RoleAll, RoleServe, RoleWorker:
		return true
	}
	return false
}

// WithRole sets the process role explicitly, overriding the GOFASTR_ROLE
// env var. Panics in NewApp if r is not one of RoleAll / RoleServe /
// RoleWorker — matching how every other invalid option fails fast at
// construction rather than silently degrading.
func WithRole(r Role) AppOption {
	return func(a *App) {
		a.roleOpt = r
		a.roleSet = true
	}
}

// resolveRole applies the documented precedence — WithRole wins, then
// GOFASTR_ROLE (case-insensitive), then RoleAll — and returns an error
// describing any unknown value so the caller (NewApp) can fail loudly
// rather than silently fall back. Read once at NewApp; later mutations
// of GOFASTR_ROLE do not affect a constructed App.
func resolveRole(opt Role, optSet bool) (Role, error) {
	if optSet {
		if !isValidRole(opt) {
			return "", fmt.Errorf("framework: invalid role %q (want all, serve, or worker)", opt)
		}
		return opt, nil
	}
	if v := os.Getenv("GOFASTR_ROLE"); v != "" {
		switch r := Role(strings.ToLower(v)); r {
		case RoleAll, RoleServe, RoleWorker:
			return r, nil
		default:
			return "", fmt.Errorf("framework: invalid GOFASTR_ROLE %q (want all, serve, or worker)", v)
		}
	}
	return RoleAll, nil
}

// Role returns the resolved process role (all/serve/worker). It is set
// once in NewApp and never changes afterward, so OnStart hooks and other
// setup code can gate their own background work on it — e.g. skipping an
// expensive warm-up that only makes sense when serving HTTP:
//
//	app.OnStart(func(ctx context.Context) error {
//	    if app.Role() == framework.RoleWorker {
//	        return nil // the worker process doesn't serve this cache
//	    }
//	    return warmRenderCache(ctx)
//	})
//
// Plain OnStart hooks are role-agnostic and run in every role; worker-
// scoped registration (cron/queue/outbox) is gated internally.
func (a *App) Role() Role { return a.role }

// runsWorkers reports whether this role runs background consumers
// (AddCron/AddQueue registrations and the outbox relay).
func (a *App) runsWorkers() bool {
	return a.role == RoleAll || a.role == RoleWorker
}

// roleHandler picks the HTTP surface Start serves: the full app router for
// all/serve, or the health-only mux for a worker process.
func (a *App) roleHandler() http.Handler {
	if a.role == RoleWorker {
		return a.workerHealthMux()
	}
	return a.router
}

// workerHealthMux is the worker role's entire HTTP surface: /healthz and
// /readyz, backed by the SAME handlers the full router mounts
// (healthHandlers), so orchestrator probes behave identically across roles.
// Nothing else is served — no entity CRUD, no OpenAPI, no discovery routes.
func (a *App) workerHealthMux() http.Handler {
	liveness, readiness := a.healthHandlers()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", readiness)
	return mux
}
