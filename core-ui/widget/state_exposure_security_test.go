package widget

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: an endpoint documented as gated must actually be gated, and
// a gate with no host-installed check must fail closed rather than open.
//
// /state is unauthenticated by default and SignalSource.Read takes no
// context, so signal values are process-global and world-readable,
// that is now stated on Definition.Signals. Widgets whose signals are
// not safe to expose set RequireSession.
func TestWidgetStateGate(t *testing.T) {
	defer SetSessionCheck(nil)

	newReq := func(def *Definition) *httptest.ResponseRecorder {
		r := router.New()
		Mount(r, def)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", def.StatePath, nil))
		return w
	}

	t.Run("ungated widget stays public", func(t *testing.T) {
		SetSessionCheck(nil)
		if got := newReq(&Definition{Name: "public"}).Code; got == http.StatusForbidden {
			t.Fatal("default widget /state should remain unauthenticated")
		}
	})

	t.Run("gated widget without a host check fails closed", func(t *testing.T) {
		SetSessionCheck(nil)
		if got := newReq(&Definition{Name: "gated1", RequireSession: true}).Code; got != http.StatusForbidden {
			t.Fatalf("no host check installed: got %d, want 403 (fail closed)", got)
		}
	})

	t.Run("gated widget refuses an unauthenticated caller", func(t *testing.T) {
		SetSessionCheck(func(*http.Request) bool { return false })
		if got := newReq(&Definition{Name: "gated2", RequireSession: true}).Code; got != http.StatusForbidden {
			t.Fatalf("got %d, want 403", got)
		}
	})

	t.Run("gated widget serves an authenticated caller", func(t *testing.T) {
		SetSessionCheck(func(*http.Request) bool { return true })
		if got := newReq(&Definition{Name: "gated3", RequireSession: true}).Code; got == http.StatusForbidden {
			t.Fatal("valid session was refused")
		}
	})
}

// Property (CHAIN1-R2): a widget's declared session posture applies to
// every route its Definition mounts. Mount wraps /state and /chrome in
// gateSession(def.RequireSession, ...) (widget.go:596-600) but registers
// the same Definition's def.RPCs handlers raw (widget.go:602-616), so the
// widget's own mutation routes — the surface most in need of the gate —
// stay reachable by callers the host's session check refuses.
// RequireSession is the Definition's only security knob; a developer who
// sets it believes the widget's surface is gated. Both routes are mounted
// by the same Mount call from the same builder chain, which is why the
// asymmetry is invisible at the call site.
func TestRequireSessionGatesWidgetRPCs(t *testing.T) {
	defer SetSessionCheck(nil)

	rpcHit := func(name string, hits *[]string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*hits = append(*hits, name)
			w.WriteHeader(http.StatusOK)
		})
	}

	// The mounted RPC surface, one case per HTTP method the Mount switch
	// registers (GET/POST/PUT/DELETE; the default arm falls to POST).
	for _, tc := range []struct{ method, op string }{
		{"GET", "fetch"},
		{"POST", "purge"},
		{"PUT", "replace"},
		{"DELETE", "drop"},
	} {
		t.Run(tc.method+" rpc refused like /state", func(t *testing.T) {
			SetSessionCheck(func(*http.Request) bool { return false })
			hits := []string{}
			r := router.New()
			Mount(r, &Definition{
				Name:           "rpcgate",
				RequireSession: true,
				RPCs: []RPCEndpoint{{
					Method:  tc.method,
					Path:    "/rpcgate/" + tc.op,
					Handler: rpcHit(tc.op, &hits),
				}},
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, "/rpcgate/"+tc.op, nil))

			if w.Code != http.StatusForbidden {
				t.Fatalf("SECURITY: [widget-rpcgate] RequireSession widget: %s /rpcgate/%s returned %d for a caller the host session check refuses, while the same widget's /state returns 403 in the same process. Attack: the widget's own mutation route bypasses the Definition's session gate (Mount registers def.RPCs without gateSession, widget.go:602-616). Handler executions: %v", tc.method, tc.op, w.Code, hits)
			}
			if len(hits) > 0 {
				t.Fatalf("SECURITY: [widget-rpcgate] RPC handler %q executed for a caller the session check refuses (handler ran before any gate could answer)", tc.op)
			}
		})
	}

	// Control: the gate predicate itself works in this process — the same
	// RequireSession flag DOES gate the sibling /state route. Isolates the
	// defect to the RPC registration hop, not a broken predicate.
	t.Run("control /state is gated", func(t *testing.T) {
		SetSessionCheck(func(*http.Request) bool { return false })
		r := router.New()
		Mount(r, &Definition{Name: "rpcgate-ctl", RequireSession: true})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/core-ui/widget/rpcgate-ctl/state", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("control: /state must be 403 under a refusing check, got %d — the predicate is broken and the RPC cases above prove nothing", w.Code)
		}
	})
}
