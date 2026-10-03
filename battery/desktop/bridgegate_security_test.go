package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// PROPERTY
//
//	A bridge route that invokes native capability handlers is closed
//	until Run has actually brought the desktop host up, and while it is
//	closed it reveals nothing about what is registered behind it.
//
// SIBLING-SINK DRIFT (the shape this pass keeps finding)
//
//	Three routes hang off /__gofastr/desktop. Two already carry the
//	guard; the third, the only one that runs native code, does not:
//
//	  serveManifest     -> frozenManifest() -> 503 before the freeze  OK
//	  serveBridgeScript -> !reg.isFrozen()  -> inert JS               OK
//	  handleCall        -> nothing                                    GAP
//
//	registerBridgeRoutes wires all three in Init, so they answer from
//	the moment the listener binds. In the documented --serve / --addr /
//	$PORT mode (examples/desktop-notes/main.go) Run never happens:
//	gateMiddleware is a pass-through, there is no boot cookie and no
//	Host pin, and handleCall accepts a request with no Sec-Fetch-Site
//	header at all (what every non-browser client sends). A $PORT
//	deployment binds a public interface, not loopback.
//
//	Measured on this tree before the fix:
//	  POST /call/windows/list       -> 200 {"ok":true,...}
//	  POST /call/window/title       -> 501 unsupported
//	  POST /call/clipboard/writeText-> 403 denied
//	  POST /call/nosuchcap/x        -> 404 not found
//	The last line is the leak: a registered capability is distinguishable
//	from an absent one, so the app's plugin inventory is enumerable by
//	anyone who can reach the port.
//
// One fix closes both: refuse with the SAME uniform 404 the missing-
// capability path already uses, until the registry is frozen.

// unfrozenCallRouter is the --serve / $PORT posture: Init wired the
// routes, Run never ran, so the registry is not frozen and the gate is
// not armed.
func unfrozenCallRouter(t *testing.T) (*Battery, *router.Router) {
	t.Helper()
	b, _ := newTestBattery(t)
	r := router.New()
	r.Post(callPattern, http.HandlerFunc(b.handleCall))
	return b, r
}

// TestCallRefusedBeforeRun pins that no handler runs before the freeze.
func TestCallRefusedBeforeRun(t *testing.T) {
	b, r := unfrozenCallRouter(t)

	reached := false
	if err := b.Register(Capability{
		Name:    "probe",
		Version: 1,
		Methods: []Method{{
			Name: "ungated",
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
				reached = true
				return map[string]any{"ok": true}, nil
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"probe/ungated", "windows/list", "window/title", "clipboard/writeText"} {
		code, body := postBridge(t, r, strings.Split(p, "/")[0], strings.Split(p, "/")[1], "{}")
		if code != http.StatusNotFound {
			t.Errorf("%s before Run: %d %s, want 404 (the host is not running)", p, code, strings.TrimSpace(body))
		}
	}
	if reached {
		t.Fatal("a capability handler ran with no desktop host started; " +
			"in --serve / $PORT mode that is an unauthenticated caller reaching the native dispatcher")
	}
}

// TestCapsNotEnumerableBeforeRun pins that the refusal is byte-identical
// for a registered and an absent capability.
func TestCapsNotEnumerableBeforeRun(t *testing.T) {
	_, r := unfrozenCallRouter(t)

	registeredCode, registeredBody := postBridge(t, r, "clipboard", "writeText", "{}")
	absentCode, absentBody := postBridge(t, r, "nosuchcap", "writeText", "{}")
	missingMethod, missingBody := postBridge(t, r, "clipboard", "nosuchmethod", "{}")

	if registeredCode != absentCode || registeredBody != absentBody {
		t.Errorf("a registered capability is distinguishable from an absent one before Run:\n"+
			"  clipboard -> %d %s\n  nosuchcap -> %d %s\n"+
			"that enumerates the app's capability (and plugin) inventory",
			registeredCode, strings.TrimSpace(registeredBody), absentCode, strings.TrimSpace(absentBody))
	}
	if missingMethod != absentCode || missingBody != absentBody {
		t.Errorf("a missing method is distinguishable from a missing capability: %d %s",
			missingMethod, strings.TrimSpace(missingBody))
	}
}

// TestCallWorksOnceRunFroze is the anti-vacuity half: the guard must not
// break the desktop posture it is protecting.
func TestCallWorksOnceRunFroze(t *testing.T) {
	b, r := unfrozenCallRouter(t)
	b.freezeForTest()

	code, body := postBridge(t, r, "windows", "list", "{}")
	if code != http.StatusOK {
		t.Fatalf("windows.list after the freeze: %d %s, want 200", code, strings.TrimSpace(body))
	}
	if absent, _ := postBridge(t, r, "nosuchcap", "x", "{}"); absent != http.StatusNotFound {
		t.Fatalf("an absent capability after the freeze: %d, want 404", absent)
	}
	_ = httptest.NewRecorder
}
