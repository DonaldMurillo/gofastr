//go:build red

package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: no credential value appears in any world the chat server hands
// out. redactedWorld is the single masking chokepoint for the world-bearing
// GET surfaces, and a credentialed DSN is a credential by the framework's
// own freeze contract: kiln/freeze/blueprint.go dbURLRef/dsnHasSecret
// replace exactly this shape (postgres://user:pass@host) with a
// ${DATABASE_URL} reference before anything durable is written, because the
// password embedded in the URL is a live database credential.
// Surfaces: kiln/chat/server.go::redactedWorld (masks only
// App.Auth.JWTSecret + App.Admin.SeedPassword; App.DBURL untouched) via
// serveWorld (GET /kiln/world) and serveStatus (?fields=app and
// ?fields=world).
// Finding: a prompt-injected agent turn that transfers a DSN the developer
// pasted into chat into set_app_config parks it in App.DBURL; redactedWorld
// copies it through verbatim, so GET /kiln/world and /kiln/status hand the
// embedded password to any local process, and to any DNS-rebinding page
// (the rebinding read class is this package's own tested threat model, see
// TestReadRoutesRefuseCrossSiteSub).
// Fix direction: extend redactedWorld to mask App.DBURL whenever
// dsnHasSecret-equivalent logic says the DSN embeds credentials (URL
// userinfo or a password= pair), leaving credential-free DSNs (file:blog.db)
// visible so local frozen apps stay diagnosable. Reuse the dsnHasSecret
// rule rather than re-deriving it, so the freeze contract and the serving
// contract cannot drift.
func TestWorldRedMasksCredentialedDSN(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-dsn-red")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)

	const canary = "dsn-canary-0123456789"
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{
			Name:     "sec",
			DBDriver: "postgres",
			DBURL:    "postgres://kiln:" + canary + "@db.internal:5432/prod",
		},
	})
	if !res.OK {
		t.Fatalf("setup broken: SetAppConfig with a credentialed DSN returned %+v — the ingestion path must accept it for this read-side test to run", res)
	}

	srv := New(l, tools)

	// GET /kiln/world: the whole in-memory IR, redacted copy mandatory.
	req := httptest.NewRequest(http.MethodGet, "/kiln/world", nil)
	rec := httptest.NewRecorder()
	srv.serveWorld(rec, req)
	if strings.Contains(rec.Body.String(), canary) {
		t.Errorf("SECURITY: [kiln-world-dsn-unredacted] GET /kiln/world leaked the DSN password %q verbatim — redactedWorld masks App.Auth.JWTSecret and App.Admin.SeedPassword but copies App.DBURL through, and a credentialed DSN is a credential by the framework's own freeze contract (dsnHasSecret): %.600s", canary, rec.Body.String())
	}

	// /kiln/status world-bearing field selectors: same masking chokepoint.
	for _, fields := range []string{"app", "world"} {
		req := httptest.NewRequest(http.MethodGet, "/kiln/status?fields="+fields, nil)
		rec := httptest.NewRecorder()
		srv.serveStatus(rec, req)
		if strings.Contains(rec.Body.String(), canary) {
			t.Errorf("SECURITY: [kiln-world-dsn-unredacted] GET /kiln/status?fields=%s leaked the DSN password %q verbatim — serveStatus routes both world-shaped fields through redactedWorld, which does not mask App.DBURL: %.600s", fields, canary, rec.Body.String())
		}
	}

	// Contrast leg: a credential-free DSN is configuration, not a secret.
	// A blanket mask that blanks every DBURL would break local frozen apps
	// (their sqlite path is how the operator verifies what will boot), so
	// the fix must be credential-shaped, not field-shaped.
	res = tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{
			Name:     "sec",
			DBDriver: "sqlite",
			DBURL:    "file:blog.db",
		},
	})
	if !res.OK {
		t.Fatalf("setup broken: SetAppConfig with a credential-free DSN returned %+v", res)
	}
	req = httptest.NewRequest(http.MethodGet, "/kiln/status?fields=app", nil)
	rec = httptest.NewRecorder()
	srv.serveStatus(rec, req)
	if !strings.Contains(rec.Body.String(), "file:blog.db") {
		t.Errorf("SECURITY: [kiln-world-dsn-unredacted] credential-free DSN %q is absent from GET /kiln/status?fields=app — masking must distinguish credentialed DSNs from local file paths, not blank the field: %.600s", "file:blog.db", rec.Body.String())
	}
}
