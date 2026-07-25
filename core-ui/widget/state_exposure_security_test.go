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
// context, so signal values are process-global and world-readable —
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
