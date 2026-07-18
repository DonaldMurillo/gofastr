package crud

import (
	"context"
	"net/http"
)

// Redispatch is the exported re-dispatch seam the process-module capability
// broker (framework/processmodule_broker.go, design #37 §5) uses to re-enter
// the CRUD chokepoint for a reverse host.entity.* call.
//
// It is the exact machinery RegisterEntityMCPTools uses (runToolRequest): it
// builds an in-process http.Request, copies the originating caller's
// Cookie/Authorization headers from the request stashed on ctx via
// mcp.WithRequest (without which owner-scoped CRUD re-resolution returns 401),
// ServeHTTP's it through router so the FULL middleware chain
// (auth/recovery/owner/tenant/permission + token-scope) re-runs, and returns
// the parsed JSON envelope. A status >= 400 is returned as an error so the
// broker can surface the CRUD chokepoint's 401/403 as a reverse-call denial.
//
// The broker owns the capability pre-filter (access.ScopeMatch module-grant +
// the CrossOwnerRead carve-out); this function is deliberately ONLY the
// caller-authority half of the module-grant ∩ caller-authority intersection.
// It performs no capability reasoning of its own.
func Redispatch(ctx context.Context, router http.Handler, method, path string, body any) (any, error) {
	return runToolRequest(ctx, router, method, path, body)
}
