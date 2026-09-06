package desktop

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// PROPERTY
//
//	A decision the USER gave is persisted and honoured, regardless of
//	what the requesting PAGE does with its HTTP connection.
//
// WHY (the deep finding, sec-auditor 2026-09-04; PROVEN by execution
// against the real sqlGrantStore over SQLite, not modelled)
//
//	authorizeCall (bridge.go) persists with the REQUEST context:
//
//	    b.grants.Set(ctx, m.Permission, grantDeny)   // ctx == r.Context()
//
//	(the pre-fix line; persistGrant now detaches the context and carries
//	the capability)
//
//	Go does not stop a handler when the client goes away; it cancels
//	r.Context(). sqlGrantStore.Set is ExecContext, so a cancelled
//	context makes the write fail. The failure is logged and the call
//	still returns "denied", it fails CLOSED for that one call, but
//	NOTHING IS STORED.
//
//	So a hostile page defeats the one control the docs promise:
//	  grants.go:14-17  "deny is a persisted refusal (so a page cannot
//	                    re-prompt in a loop)"
//	  shell.go:45-47   "DecisionDeny ... persists the refusal, so a page
//	                    cannot re-prompt in a loop"
//
//	  fetch(url, {signal}); setTimeout(() => ctrl.abort(), 50);
//
//	The alert is already on screen when the abort lands (Prompt only
//	checks ctx.Err() on entry). The user clicks Deny, the write is
//	dropped, and the page asks again. Measured on this tree: two alerts
//	for one permission, and the desktop_grants row never appears.
//
//	The same drop hits DecisionAllow, which merely re-prompts, so the
//	deny direction is the security-relevant one.
//
// FIX SHAPE: persist on a context detached from the request
// (context.WithoutCancel + a short timeout). The decision belongs to
// the USER and the SHELL, not to the page's socket.
//
// SECOND PROPERTY IN THIS FILE (D-08): an authorization decision is
// keyed on the full subject it was granted for. The OS alert names the
// Capability, the Method AND the Permission, but only the Permission
// string is stored, so a second capability declaring the same
// permission string inherits a grant the user gave a different, named
// surface, with no prompt, across app upgrades.

// promptCancelShell aborts the request context while the alert is on
// screen, which is exactly what a page-side AbortController does.
type promptCancelShell struct {
	*fakeShell
	cancel   func()
	decision Decision
}

func (s *promptCancelShell) Prompt(ctx context.Context, req PermissionRequest) (Decision, error) {
	s.fakeShell.mu.Lock()
	s.fakeShell.promptRequests = append(s.fakeShell.promptRequests, req)
	s.fakeShell.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return s.decision, nil
}

// newSQLiteGrants opens a real sqlGrantStore. The mem store ignores its
// context entirely, so only the SQL store, the one every real desktop
// app gets through AppOptions, can show this bug.
func newSQLiteGrants(t *testing.T) *sqlGrantStore {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "grants.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := newSQLGrantStore(db)
	if err := store.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

// methodNamed finds one registered method for the gate tests.
func methodNamed(t *testing.T, b *Battery, capName, method string) (Capability, Method) {
	t.Helper()
	c, ok := b.reg.lookup(capName)
	if !ok {
		t.Fatalf("capability %q is not registered", capName)
	}
	for _, m := range c.Methods {
		if m.Name == method {
			return c, m
		}
	}
	t.Fatalf("method %s.%s is not registered", capName, method)
	return Capability{}, Method{}
}

// TestDenyPersistsWhenPageAborts is the core pin.
func TestDenyPersistsWhenPageAborts(t *testing.T) {
	b, fake := newTestBattery(t)
	b.grants = newSQLiteGrants(t)
	ctx, cancel := context.WithCancel(context.Background())
	b.shell = &promptCancelShell{fakeShell: fake, cancel: cancel, decision: DecisionDeny}

	c, m := methodNamed(t, b, "clipboard", "writeText")

	if proceed, _ := b.authorizeCall(ctx, c, m); proceed {
		t.Fatal("a denied call proceeded")
	}
	// A second call on a LIVE context must be answered from the stored
	// deny, with no new alert.
	if proceed, code := b.authorizeCall(context.Background(), c, m); proceed || code != CodeDenied {
		t.Fatalf("second call: proceed=%v code=%q, want false/denied from the persisted deny", proceed, code)
	}
	if got := len(fake.prompts()); got != 1 {
		t.Fatalf("%d permission alerts for one permission: the page aborted its fetch and the DENY was never "+
			"stored, so the documented \"deny persists, a page cannot re-prompt in a loop\" control is gone", got)
	}
}

// TestAllowPersistsWhenPageAborts pins the same detachment on the allow
// side, so a user who granted once is not asked again.
func TestAllowPersistsWhenPageAborts(t *testing.T) {
	b, fake := newTestBattery(t)
	b.grants = newSQLiteGrants(t)
	ctx, cancel := context.WithCancel(context.Background())
	b.shell = &promptCancelShell{fakeShell: fake, cancel: cancel, decision: DecisionAllow}

	c, m := methodNamed(t, b, "clipboard", "writeText")

	if proceed, code := b.authorizeCall(ctx, c, m); !proceed {
		t.Fatalf("an allowed call was refused: code=%q", code)
	}
	if proceed, _ := b.authorizeCall(context.Background(), c, m); !proceed {
		t.Fatal("second call was refused; the allow did not persist")
	}
	if got := len(fake.prompts()); got != 1 {
		t.Fatalf("%d alerts: the ALLOW was dropped with the page's connection", got)
	}
}

// TestAllowOnceStillNeverPersists is the anti-vacuity half: the fix must
// not accidentally persist the decision that is deliberately transient.
func TestAllowOnceStillNeverPersists(t *testing.T) {
	b, fake := newTestBattery(t)
	b.grants = newSQLiteGrants(t)
	fake.promptDecisions = []Decision{DecisionAllowOnce, DecisionAllowOnce}

	c, m := methodNamed(t, b, "clipboard", "writeText")
	for i := 0; i < 2; i++ {
		if proceed, _ := b.authorizeCall(context.Background(), c, m); !proceed {
			t.Fatalf("call %d was refused", i)
		}
	}
	if got := len(fake.prompts()); got != 2 {
		t.Fatalf("%d alerts for two allow-once calls, want 2: allow-once must never persist", got)
	}
}

// TestGrantIsKeyedOnItsCapability pins that consent given for one named
// capability is not silently inherited by another that happens to
// declare the same permission string.
func TestGrantIsKeyedOnItsCapability(t *testing.T) {
	b, fake := newTestBattery(t)
	b.grants = newSQLiteGrants(t)
	if err := b.Register(Capability{
		Name:    "sidecar",
		Version: 1,
		Methods: []Method{{
			Name:       "readText",
			Permission: "fs:read", // the SAME string the core fs capability uses
			Handler:    func(context.Context, json.RawMessage) (any, error) { return nil, nil },
		}},
	}); err != nil {
		t.Fatal(err)
	}
	fake.promptDecisions = []Decision{DecisionAllow, DecisionAllow}

	coreCap, coreM := methodNamed(t, b, "fs", "readText")
	if proceed, _ := b.authorizeCall(context.Background(), coreCap, coreM); !proceed {
		t.Fatal("the core fs.readText grant was refused")
	}
	sideCap, sideM := methodNamed(t, b, "sidecar", "readText")
	b.authorizeCall(context.Background(), sideCap, sideM)

	if got := len(fake.prompts()); got != 2 {
		t.Fatalf("%d alerts: the user allowed %q and a DIFFERENT capability (%q) inherited that consent with no "+
			"prompt. The alert names the capability; the store keys only on the permission string",
			got, coreCap.Name, sideCap.Name)
	}
}
