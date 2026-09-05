package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the process-local form flash breaking the multi-replica statelessness
// contract, found by the 2026-09-04 red-probe round; fixed by carrying the
// flash in a short-lived HMAC-signed cookie (Config.Secret → HKDF key, the
// sessions pattern) capped at 4 KiB, so any replica holding the app secret
// renders a redirect issued by any other and no server RAM is involved.
// Family: F8 multi-replica statelessness
// Property: a redirect-mediated handoff resolves on any replica that shares the app secret; an unverifiable flash fails closed to an empty form, never an attacker-chosen one.
// Surfaces: battery/admin/entity_admin.go::setFlash/readFlash (signed cookie, 4 KiB cap, token match),
//           battery/admin/entity_admin.go::entitySave (303 with ?e=<token> + Set-Cookie),
//           battery/admin/entity_screens.go::entityFormScreen.Load (reads the cookie via RequestFromContext),
//           battery/admin/admin.go::Config.Secret (shared-secret knob, GOFASTR_SECRET parity).

// flashTestSecret stands in for the deployment-wide app secret every replica
// shares (GOFASTR_SECRET / framework.WithSecret); ≥16 chars so the battery
// derives a real key rather than self-minting a per-boot one.
const flashTestSecret = "admin-flash-test-secret-0123456789"

// TestFlashResolvesOnSecondReplica: a validation-failed save redirected by
// replica A renders on replica B (same secret, different process-local
// battery instance) with the submitted values and field errors intact.
func TestFlashResolvesOnSecondReplica(t *testing.T) {
	db := newDB(t)
	// Two apps over one database: the multi-replica shape. Each mounts its
	// own admin battery; the flash travels in the signed cookie, not RAM.
	appA := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	appB := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	replicaA := mountEntityAdmin(t, appA, Config{Entities: []string{"posts"}, Secret: flashTestSecret}, testUser{"u1"})
	replicaB := mountEntityAdmin(t, appB, Config{Entities: []string{"posts"}, Secret: flashTestSecret}, testUser{"u1"})

	// A save that fails validation on replica A redirects with a flash token.
	rr := postForm(replicaA, "/admin/e/posts/_create", url.Values{"title": {""}, "body": {"kept-note"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Fatalf("redirect carries no flash token: %q", loc)
	}

	// The browser follows the redirect; with round-robin DNS it lands on
	// replica B, carrying the flash cookie.
	followed := followFlashRedirect(replicaB, rr, loc)
	if followed.Code != http.StatusOK {
		t.Fatalf("follow redirect on replica B: %d", followed.Code)
	}
	if !strings.Contains(followed.Body.String(), "kept-note") {
		t.Fatalf("SECURITY: [admin-flash] replica B rendered the redirected form without the flash payload (submitted value %q lost): "+
			"the flash must travel in a cookie signed with the shared app secret, not in replica A's RAM", "kept-note")
	}
}

// TestFlashCookieFailsClosedOnForeignSecret: a replica holding a DIFFERENT
// secret must render the redirect as an empty form, never apply a flash it
// cannot verify (a tampered or foreign cookie is not trusted input).
func TestFlashCookieFailsClosedOnForeignSecret(t *testing.T) {
	db := newDB(t)
	appA := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	appC := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	replicaA := mountEntityAdmin(t, appA, Config{Entities: []string{"posts"}, Secret: flashTestSecret}, testUser{"u1"})
	replicaC := mountEntityAdmin(t, appC, Config{Entities: []string{"posts"}, Secret: "a-completely-different-secret-9876"}, testUser{"u1"})

	rr := postForm(replicaA, "/admin/e/posts/_create", url.Values{"title": {""}, "body": {"kept-note"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")

	followed := followFlashRedirect(replicaC, rr, loc)
	if followed.Code != http.StatusOK {
		t.Fatalf("follow redirect on foreign replica: %d", followed.Code)
	}
	if strings.Contains(followed.Body.String(), "kept-note") {
		t.Fatal("SECURITY: [admin-flash] a replica with a different secret applied a flash it cannot verify: an unverifiable flash must fail closed to an empty form")
	}
}

// TestFlashTokenMismatchIgnored: the flash applies only when the cookie's
// embedded token matches the ?e= query token, so a stale cookie from an
// older failed submit never leaks into an unrelated form render.
func TestFlashTokenMismatchIgnored(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}, Secret: flashTestSecret}, testUser{"u1"})

	rr := postForm(h, "/admin/e/posts/_create", url.Values{"title": {""}, "body": {"stale-note"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")

	// Same cookie, wrong query token: the form renders without the flash.
	followed := followFlashRedirect(h, rr, loc+"x")
	if followed.Code != http.StatusOK {
		t.Fatalf("follow: %d", followed.Code)
	}
	if strings.Contains(followed.Body.String(), "stale-note") {
		t.Fatal("SECURITY: [admin-flash] a flash cookie was applied against a mismatched ?e= token: the token match is what keeps a stale cookie out of unrelated renders")
	}
}

// TestFlashCookieCapsOversizedPayload: a flash larger than the 4 KiB cap
// drops the submitted values but keeps the error, so the re-render always
// names the failure and the cookie can never grow unbounded.
func TestFlashCookieCapsOversizedPayload(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}, Secret: flashTestSecret}, testUser{"u1"})

	huge := strings.Repeat("x", 8<<10) // 8 KiB body value, past the 4 KiB cap
	rr := postForm(h, "/admin/e/posts/_create", url.Values{"title": {""}, "body": {huge}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	for _, c := range rr.Result().Cookies() {
		if len(c.Value) > 6<<10 {
			t.Fatalf("flash cookie value is %d bytes, past the 4 KiB payload cap", len(c.Value))
		}
	}
	loc := rr.Header().Get("Location")

	followed := followFlashRedirect(h, rr, loc)
	if followed.Code != http.StatusOK {
		t.Fatalf("follow: %d", followed.Code)
	}
	if strings.Contains(followed.Body.String(), huge) {
		t.Fatal("SECURITY: [admin-flash] an oversized flash retained its submitted values: the cap must drop the values, not ship an unbounded cookie")
	}
	if !strings.Contains(followed.Body.String(), "validation failed") {
		t.Fatal("oversized flash lost the error too: the cap keeps the error (here the CRUD validation message) and drops the values")
	}
}
