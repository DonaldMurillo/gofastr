package access_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// TestContext_NilContext verifies GetPermissions handles a nil context
// without panicking. A handler can be called with a nil context only
// in malformed test setups, but the runtime cost of guarding it is
// trivial and the failure mode (process crash) is severe.
func TestContext_NilContext(t *testing.T) {
	t.Parallel()
	var perms []access.Permission
	panicked := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		//nolint:staticcheck // intentionally passing nil to verify guard
		perms = access.GetPermissions(nil)
		panicked = false
	}()
	if panicked {
		t.Fatalf("SECURITY: [context-nil] GetPermissions(nil) panicked. Attack: nil-context DoS.")
	}
	if perms != nil {
		t.Errorf("SECURITY: [context-nil] GetPermissions(nil) returned %v, want nil.", perms)
	}
}

// TestRBAC_ConcurrentGrant exercises Grant from many goroutines.
// Go's concurrent-map writes trigger an unrecoverable runtime fatal,
// so we run the body in a subprocess and check its exit status.
func TestRBAC_ConcurrentGrant(t *testing.T) {
	t.Parallel()
	if os.Getenv("GOFASTR_SUB_GRANT") == "1" {
		rp := access.NewRolePolicy()
		for round := range 20 {
			var wg sync.WaitGroup
			for i := range 100 {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					rp.Grant("worker", access.Permission(fmt.Sprintf("perm:r%d:%d", round, n)))
				}(i)
			}
			wg.Wait()
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRBAC_ConcurrentGrant$")
	cmd.Env = append(os.Environ(), "GOFASTR_SUB_GRANT=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [rbac-concurrent] concurrent Grant() crashed: %v\n%s", err, out)
	}
}

// TestRBAC_ConcurrentRevoke exercises Revoke under contention.
func TestRBAC_ConcurrentRevoke(t *testing.T) {
	t.Parallel()
	if os.Getenv("GOFASTR_SUB_REVOKE") == "1" {
		rp := access.NewRolePolicy()
		for i := range 50 {
			rp.Grant("worker", access.Permission(fmt.Sprintf("perm:%d", i)))
		}
		for range 20 {
			var wg sync.WaitGroup
			for i := range 50 {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					rp.Revoke("worker", access.Permission(fmt.Sprintf("perm:%d", n)))
				}(i)
			}
			wg.Wait()
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRBAC_ConcurrentRevoke$")
	cmd.Env = append(os.Environ(), "GOFASTR_SUB_REVOKE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [rbac-concurrent] concurrent Revoke() crashed: %v\n%s", err, out)
	}
}

// TestRBAC_ConcurrentGrantRevokeWithReads verifies that simultaneous
// Grant, Revoke, and read traffic on the same role do not race or lose
// the role entirely. Run under -race to amplify any remaining bug.
func TestRBAC_ConcurrentGrantRevokeWithReads(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("worker", "base:read")
	ctx := access.WithRoles(access.WithPolicy(context.Background(), rp), []string{"worker"})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			rp.Grant("worker", access.Permission(fmt.Sprintf("g:%d", n)))
		}(i)
		go func(n int) {
			defer wg.Done()
			rp.Revoke("worker", access.Permission(fmt.Sprintf("g:%d", n)))
		}(i)
		go func() {
			defer wg.Done()
			_ = access.GetPermissions(ctx)
		}()
	}
	wg.Wait()
}

// TestCanWildcardGrantsOnlyGlobalStar pins the Can hot path's wildcard
// posture: a permission check passes only on an exact match or the global
// "*" (Wildcard). Partial wildcards a caller managed to grant — "posts:*",
// "*:read" — with an empty capability registry (warn mode keeps them as
// literals) must NOT widen into matching other permissions, both on
// Can and on the RequirePermission middleware.
func TestCanWildcardGrantsOnlyGlobalStar(t *testing.T) {
	t.Parallel()
	policy := access.NewRolePolicy() // empty registry: warn mode, literals stay unexpanded
	for _, grant := range []access.Permission{"posts:*", "*:read"} {
		if err := policy.Grant("editor", grant); err != nil {
			t.Fatalf("Grant(%s): %v", grant, err)
		}
	}
	ctx := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"editor"})

	if access.Can(ctx, access.Permission("posts:read")) {
		t.Fatalf("SECURITY: [rbac-wildcard] role holding %q passed Can(posts:read): partial wildcards must never match",
			"posts:*")
	}
	if access.Can(ctx, access.Permission("users:read")) {
		t.Fatalf("SECURITY: [rbac-wildcard] role holding %q passed Can(users:read): partial wildcards must never match",
			"*:read")
	}

	// RequirePermission must agree (403, not 404/500).
	h := access.RequirePermission(access.Permission("posts:read"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [rbac-wildcard] RequirePermission with partial-wildcard grants returned %d, want 403",
			rec.Code)
	}

	// The global "*" passes everything — the one deliberate wildcard.
	star := access.NewRolePolicy()
	if err := star.Grant("sudo", access.Wildcard); err != nil {
		t.Fatalf("Grant(*): %v", err)
	}
	starCtx := access.WithRoles(access.WithPolicy(context.Background(), star), []string{"sudo"})
	if !access.Can(starCtx, access.Permission("posts:read")) {
		t.Fatal("global \"*\" must pass any permission check")
	}
}

// TestRequirePermissionHonorsDecider: a resource-scoped Decider's Deny is
// honoured at every permission gate, not only at CanResource call sites.
// RequirePermission offers the zero Ref (a route gate holds no record)
// and a DecisionDeny on it fails closed with 403 even when the role
// policy grants the permission.
func TestRequirePermissionHonorsDecider(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Register("projects:update")
	if err := policy.Grant("editor", "projects:update"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Deny on the projects resource and on the zero Ref: a route-level gate
	// like RequirePermission has no record in hand, so it can only ever
	// offer the collection-level (zero) Ref, which a decider must not
	// silently wave through when its rule for that resource says deny.
	denier := func(_ context.Context, _ []string, _ access.Permission, res access.Ref) access.Decision {
		if res.Type == "projects" || res.Type == "" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Chain exactly as DeciderMiddleware's doc comment prescribes: policy
	// and roles outermost, decider alongside, RequirePermission gating the
	// route.
	h := access.Middleware(policy, func(context.Context) []string { return []string{"editor"} })(
		access.DeciderMiddleware(denier)(
			access.RequirePermission("projects:update")(ok),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/projects/7", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [decider-deny] RequirePermission returned %d with a DecisionDeny decider installed via DeciderMiddleware, want 403 — the decider seam must be consulted by this gate", rec.Code)
	}
}
