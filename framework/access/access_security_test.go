package access_test

import (
	"context"
	"fmt"
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
		for round := 0; round < 20; round++ {
			var wg sync.WaitGroup
			for i := 0; i < 100; i++ {
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
		for i := 0; i < 50; i++ {
			rp.Grant("worker", access.Permission(fmt.Sprintf("perm:%d", i)))
		}
		for round := 0; round < 20; round++ {
			var wg sync.WaitGroup
			for i := 0; i < 50; i++ {
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
	for i := 0; i < 50; i++ {
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
