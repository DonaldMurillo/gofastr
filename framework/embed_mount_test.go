package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// embedMountable is the shape App.Mount duck-types on to hand an embed host its
// signing keys, the same seam SetFanout and SetSessionKey use.
type embedMountable struct{ host *embed.Host }

func (m *embedMountable) Mount(*router.Router)   {}
func (m *embedMountable) EmbedHost() *embed.Host { return m.host }

func newTestEmbedHost(t *testing.T) *embed.Host {
	t.Helper()
	h, err := embed.New(embed.Config{
		Surfaces:  []embed.Surface{{Name: "reports", Screen: fwTestScreen{"/reports"}, Origins: []string{"https://acme.com"}}},
		BurnStore: embed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	return h
}

func TestMountDerivesEmbedKeysFromTheAppSecret(t *testing.T) {
	host := newTestEmbedHost(t)
	if host.Ready() {
		t.Fatal("a freshly constructed host already has keys")
	}

	app := NewApp(WithSecret("mount-test-secret-mount-test-sec"))
	app.Mount(&embedMountable{host: host})

	if !host.Ready() {
		t.Fatal("Mount did not hand the embed host its signing keys")
	}
	// The keys must actually work end to end, not merely be non-empty.
	if _, err := host.MintNonce(context.Background(), "reports", "u-1", "https://acme.com", nil); err != nil {
		t.Fatalf("MintNonce with the derived key: %v", err)
	}
}

// An embed host with no app secret is a boot failure, not a warning. A nonce
// that fails to verify is gone, single-use, one-minute life, already rendered
// into a page on someone else's site that the app cannot re-render. The
// self-minted per-boot key that makes sessions survive a restart would make
// every outstanding nonce die on one.
func TestMountPanicsOnAnEmbedHostWithoutASecret(t *testing.T) {
	host := newTestEmbedHost(t)
	app := NewApp()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mounting an embed host with no app secret did not panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "secret") {
			t.Fatalf("panic message does not name the missing secret: %v", r)
		}
	}()
	app.Mount(&embedMountable{host: host})
}

// A Mountable that exposes the seam but carries no host must not be treated as
// an embed app, an ordinary UI host on an app with no secret still mounts.
func TestMountIgnoresANilEmbedHost(t *testing.T) {
	app := NewApp()
	app.Mount(&embedMountable{host: nil})
}

// Idempotency is installed by NewApp, so it sits OUTSIDE embeds.Middleware()
// and runs before any grant is verified. That left ownerPrincipal returning
// "anon" for every embed request, putting all embed subjects in one namespace:
// two grant holders sending the same Idempotency-Key got each other's stored
// responses, and the second handler never ran.
func TestIdempotencyNamespaceSeparatesEmbedSubjects(t *testing.T) {
	principalFor := func(grant string) string {
		req := httptest.NewRequest(http.MethodPost, "/reports/save", nil)
		if grant != "" {
			req.Header.Set("X-Gofastr-Embed", grant)
		}
		return ownerPrincipal(req)
	}

	alice := principalFor("emg_alice-grant")
	bob := principalFor("emg_bob-grant")

	if alice == bob {
		t.Fatalf("two different grants share the idempotency namespace (%q).\n"+
			"Both subjects POSTing with the same Idempotency-Key means the second "+
			"receives the first's stored response and its handler never runs.", alice)
	}
	if alice == "anon" || bob == "anon" {
		t.Errorf("a grant-bearing request was namespaced as %q", alice)
	}
	// An ordinary anonymous request is still "anon", the change must not
	// fragment the namespace for callers that legitimately share it.
	if got := principalFor(""); got != "anon" {
		t.Errorf("anonymous principal = %q, want anon", got)
	}
	// The same grant is stable, or a legitimate retry would never match.
	if principalFor("emg_alice-grant") != alice {
		t.Error("the same grant produced two namespaces; a retry could never replay")
	}
}

// The same defect, one layer wider: the owner branch above is inert on the
// default wiring (idempotency is installed by NewApp, so it runs outside the
// auth middleware the app adds), which put every AUTHENTICATED SESSION in the
// "anon" namespace too, not just embed requests.
func TestIdempotencyNamespaceSeparatesOrdinaryCallers(t *testing.T) {
	with := func(mutate func(*http.Request)) string {
		req := httptest.NewRequest(http.MethodPost, "/orders", nil)
		mutate(req)
		return ownerPrincipal(req)
	}

	alice := with(func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "session_id", Value: "alice-session"})
	})
	bob := with(func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "session_id", Value: "bob-session"})
	})
	bearer := with(func(r *http.Request) { r.Header.Set("Authorization", "Bearer alice-jwt") })
	anon := with(func(*http.Request) {})

	if alice == bob {
		t.Errorf("two sessions share the idempotency namespace (%q) — the second "+
			"caller receives the first's stored response", alice)
	}
	if bearer == alice || bearer == anon {
		t.Errorf("a bearer caller shares a namespace: bearer=%q alice=%q anon=%q", bearer, alice, anon)
	}
	if anon != "anon" {
		t.Errorf("an uncredentialed request = %q, want anon", anon)
	}
	// Stability: a retry with the same credential must land in the same
	// namespace, or replay protection never engages at all.
	if with(func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "session_id", Value: "alice-session"})
	}) != alice {
		t.Error("the same credential produced two namespaces; a retry could never replay")
	}
}
