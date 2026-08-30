package framework

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// Role selects which responsibilities a single binary assumes at boot.
// One binary, role picked at deploy time, so background load (cron,
// queue workers, the outbox relay) can run in a dedicated process that
// doesn't share a listener with request serving.
//
// Resolution precedence (see resolveRole): WithRole > the GOFASTR_ROLE
// env var > RoleAll. An unknown value in either place fails loudly in
// NewApp, a typo'd role must never silently run the wrong workload.
type Role string

const (
	// RoleAll is the default: serve HTTP AND run background consumers
	// (cron, queues, the outbox relay). Exactly today's behavior, zero
	// change for existing apps.
	RoleAll Role = "all"

	// RoleServe runs the full HTTP surface (router, auto-migrate, seeds,
	// plugins, batteries) but does NOT start worker-scoped consumers:
	// AddCron/AddQueue registrations and the outbox relay are skipped, so
	// a serve-only process never starts, nor later tries to drain, a
	// scheduler it never owned. Plain OnStart hooks still run; gate your
	// own via App.Role().
	RoleServe Role = "serve"

	// RoleWorker runs background consumers (cron, queues, outbox relay)
	// and binds addr, but serves ONLY the health surface (/healthz +
	// /readyz). It does NOT mount the app router, entity CRUD, OpenAPI,
	// docs, admin, or well-known discovery routes. Auto-migrate, seeds,
	// plugins, and batteries still run: migrations take their own advisory
	// lock and seeds take a SEPARATE one, so either process type may boot
	// first without racing schema or seed writes.
	RoleWorker Role = "worker"

	// RoleAgent serves the agent surface only: the /mcp mount (POST
	// JSON-RPC + GET SSE, plus its spec-reserved subpaths such as
	// /mcp/server-card) and the health endpoints. Entity CRUD routes,
	// OpenAPI, docs, admin, and well-known discovery are NOT served, and
	// worker-scoped consumers (cron/queue/outbox relay) do not start,
	// like RoleServe. Use it to give agent traffic (a tunnel, an
	// allow-listed LB) a dedicated, narrow listener without exposing the
	// browser surface. The /mcp requests still run through the app
	// router's middleware chain, so session/bearer auth and owner
	// scoping behave exactly as they do on a full serve process.
	RoleAgent Role = "agent"
)

// isValidRole reports whether r is one of the defined Role constants.
func isValidRole(r Role) bool {
	switch r {
	case RoleAll, RoleServe, RoleWorker, RoleAgent:
		return true
	}
	return false
}

// WithRole sets the process role explicitly, overriding the GOFASTR_ROLE
// env var. Panics in NewApp if r is not one of RoleAll / RoleServe /
// RoleWorker / RoleAgent, matching how every other invalid option fails
// fast at construction rather than silently degrading.
func WithRole(r Role) AppOption {
	return func(a *App) {
		a.roleOpt = r
		a.roleSet = true
	}
}

// resolveRole applies the documented precedence. WithRole wins, then
// GOFASTR_ROLE (case-insensitive), then RoleAll, and returns an error
// describing any unknown value so the caller (NewApp) can fail loudly
// rather than silently fall back. Read once at NewApp; later mutations
// of GOFASTR_ROLE do not affect a constructed App.
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

// Role returns the resolved process role (all/serve/worker/agent). It is set
// once in NewApp and never changes afterward, so OnStart hooks and other
// setup code can gate their own background work on it, e.g. skipping an
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
// all/serve, the health-only mux for a worker process, or the MCP+health
// mux for an agent process.
func (a *App) roleHandler() http.Handler {
	if a.role == RoleWorker {
		return a.workerHealthMux()
	}
	if a.role == RoleAgent {
		return a.agentMux()
	}
	return a.router
}

// workerHealthMux is the worker role's entire HTTP surface: /healthz and
// /readyz, backed by the SAME handlers the full router mounts
// (healthHandlers), so orchestrator probes behave identically across roles.
// Nothing else is served, no entity CRUD, no OpenAPI, no discovery routes.
func (a *App) workerHealthMux() http.Handler {
	liveness, readiness := a.healthHandlers()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", readiness)
	return mux
}

// agentMux is the agent role's entire HTTP surface: /healthz and /readyz
// (the same handlers the full router mounts) plus the /mcp mount and its
// spec-reserved subpaths (/mcp/server-card), forwarded to the app router
// so /mcp requests run under the SAME middleware chain — auth, owner
// scoping context, recovery — they see in a full serve process. Only
// /mcp paths are forwarded: entity CRUD, OpenAPI, docs, admin, and
// well-known discovery stay unreachable from an agent-role listener even
// though the router still holds them. A host that never mounted /mcp
// (no WithMCP, no hand-wired route) gets the router's 404 here, which is
// the honest answer for that misconfiguration.
//
// The MCP Apps widget client script (mcp.WidgetClientScriptURL) is
// forwarded too: a widget document fetches it from the same public
// origin that serves /mcp, and in a role-split deployment that origin is
// this listener. Refusing it here would 404 every widget exactly when
// the agent role is the MCP endpoint. Same honesty rule: an app that
// mounted nothing there (no WithMCPApp) gets the router's 404.
func (a *App) agentMux() http.Handler {
	liveness, readiness := a.healthHandlers()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/readyz", readiness)
	mux.Handle("/mcp", a.router)
	mux.Handle("/mcp/", a.router)
	mux.Handle(mcp.WidgetClientScriptURL, a.router)
	return mux
}
