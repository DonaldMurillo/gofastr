//go:build red

package admin

// RED TESTS — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: a cookie-authenticated form mutation must enforce same-origin /
// CSRF at the handler or at middleware the battery itself mandates. A POST the
// victim's browser can be tricked into sending (attacker page on a sibling
// subdomain: same-site, so the SameSite session cookie — Strict OR Lax —
// attaches, but cross-origin, so only the attacker's origin is proven) must be
// refused, not executed.
// Surfaces: every admin form mutation is a bare urlencoded POST behind only
// b.gate: rbac_admin.go handleRBACGrant/Revoke/Assign, process_modules.go
// handleModuleEnable/Disable/Bump/Revoke, admin.go handleQueueReplay,
// entity_admin.go entitySave. The screens render a hidden _csrf input from
// middleware.TokenFromContext (rbac_admin.go:68, process_modules.go:70,
// admin.go:457, entity_screens.go) but NO handler verifies it, and there is no
// empty-token fallback. RegisterRoutes (admin.go:323-364) wraps handlers with
// SecurityHeaders + b.gate only — that IS the battery's own default mounting
// posture; the CSRF middleware that would mint/verify the token is an optional
// app-level add-on the generated-app blueprint deliberately omits by default
// (docs blueprints.md:898-909, pinned TestAuthCSRFGapCommentEmitted). So under
// the default posture every admin mutation renders an EMPTY _csrf input and
// accepts any forged POST that clears the auth gate.
// Finding: with a signed-in operator (context user below stands in for the
// session cookie the framework auth chain already validated), a cross-site
// POST — Sec-Fetch-Site: same-site + Origin host ≠ request host, the exact
// sibling-subdomain shape battery/auth's rejectCrossSiteForm treats as an
// attack (core.go:95-123) — is executed verbatim: grants persist, modules
// toggle, dead jobs re-fire, entity rows change. No existing pin covers this:
// rbac_admin_red_test pins privilege tiering, entity_admin_red_test pins mask
// fail-closed, admin_security_test pins the auth gate + response headers.
// Fix direction: apply the rejectCrossSiteForm equivalent (Sec-Fetch-Site
// first, Origin-host fallback, isForgeableRequest shape) to the mutating
// admin handlers or wrap them in RegisterRoutes, and/or refuse any mutating
// POST whose TokenFromContext is empty so the battery fails closed when the
// optional middleware is not mounted.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// redFormPost posts an admin form as the (already authenticated) stand-in
// admin user. crossSite=true reproduces the sibling-subdomain attack: the
// browser sends Sec-Fetch-Site: same-site (true — both hosts share the
// registrable domain) and Origin: https://evil.example.com (≠ request host),
// and the SameSite cookie attaches. crossSite=false is the same-origin
// positive-control shape. The attack never carries a _csrf field: the battery
// cannot rely on one, its own screens render it empty without the optional
// middleware.
func redFormPost(h http.Handler, user any, path string, vals url.Values, crossSite bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "app.example.com"
	if crossSite {
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Origin", "https://evil.example.com")
	} else {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "https://app.example.com")
	}
	if user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), user))
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// redRefused reports whether rr is a client-error refusal of the mutation.
func redRefused(rr *httptest.ResponseRecorder) bool {
	return rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden
}

func TestRbacAdminRedEnforcesCSRF(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("grant EnsureSchema: %v", err)
	}
	if err := store.LoadInto(ctx, policy); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	h := mountAdmin(t, Config{DB: db, Policy: policy, GrantStore: store})

	// Positive control: a same-origin grant reaches the RPC end to end —
	// otherwise the harness, not the seam, is what refuses.
	if rr := redFormPost(h, nil, "/admin/rbac/_grant",
		url.Values{"role": {"editor"}, "permission": {"posts:write"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin grant got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	if !slices.Contains(policy.PermissionsOf("editor"), access.Permission("posts:write")) {
		t.Fatalf("setup: same-origin grant did not persist — harness broken, not the seam")
	}

	// The attack: a sibling-subdomain page auto-submits a grant with no
	// _csrf; the operator's session cookie rides the same-site POST.
	rr := redFormPost(h, nil, "/admin/rbac/_grant",
		url.Values{"role": {"editor"}, "permission": {"users:delete"}}, true)
	if !redRefused(rr) || slices.Contains(policy.PermissionsOf("editor"), access.Permission("users:delete")) {
		t.Errorf("SECURITY: [admin-csrf-rbac] cross-site POST /admin/rbac/_grant (Sec-Fetch-Site: same-site, Origin: evil.example.com, no _csrf) returned %d and the grant %v — "+
			"handleRBACGrant verifies neither token nor origin, so under the battery's default mounting (RegisterRoutes = gate + SecurityHeaders, optional CSRF middleware absent, "+
			"a rendered-but-empty hidden _csrf) a forged urlencoded POST mints permissions on a signed-in operator's session",
			rr.Code, policy.PermissionsOf("editor"))
	}
}

func TestEntitySaveRedEnforcesCSRF(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	if _, err := db.Exec(`INSERT INTO posts (id, title, body, published, status) VALUES ('p1', 'Before', 'b', 0, 'draft')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// Positive control: same-origin save updates the row (303 + persisted).
	if rr := redFormPost(h, nil, "/admin/e/posts/_update/p1",
		url.Values{"title": {"Legit"}, "status": {"draft"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin update got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title); err != nil || title != "Legit" {
		t.Fatalf("setup: same-origin update not persisted (title=%q err=%v) — harness broken, not the seam", title, err)
	}

	// The attack: cross-origin same-site POST rewrites the row silently.
	rr := redFormPost(h, nil, "/admin/e/posts/_update/p1",
		url.Values{"title": {"PWNED"}, "status": {"published"}}, true)
	_ = db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title)
	if !redRefused(rr) || title != "Legit" {
		t.Errorf("SECURITY: [admin-csrf-entity] cross-site POST /admin/e/posts/_update/p1 (Sec-Fetch-Site: same-site, Origin: evil.example.com, no _csrf) returned %d and the row now reads %q — "+
			"entitySave ParseForm→formToJSON→callCrud checks neither token nor origin, so a forged urlencoded POST rewrites back-office rows on a signed-in operator's session",
			rr.Code, title)
	}
}

func TestModuleToggleRedEnforcesCSRF(t *testing.T) {
	fake := &fakeModuleController{}
	_, r, _ := moduleTestEnv(t, fake)

	// Positive control: same-origin enable reaches the controller.
	if rr := redFormPost(r, roleUser{roles: []string{"admin"}}, "/admin/modules/_enable",
		url.Values{"module": {"billing"}}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin enable got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	if !slices.Contains(fake.enabled, "billing") {
		t.Fatalf("setup: same-origin enable not applied (enabled=%v) — harness broken, not the seam", fake.enabled)
	}

	// The attack: cross-origin same-site POST enables a second module.
	rr := redFormPost(r, roleUser{roles: []string{"admin"}}, "/admin/modules/_enable",
		url.Values{"module": {"payments"}}, true)
	if !redRefused(rr) || slices.Contains(fake.enabled, "payments") {
		t.Errorf("SECURITY: [admin-csrf-modules] cross-site POST /admin/modules/_enable (Sec-Fetch-Site: same-site, Origin: evil.example.com, no _csrf) returned %d and enabled=%v — "+
			"handleModuleEnable verifies neither token nor origin, so a forged urlencoded POST toggles process modules (each toggle also bumps generations and restarts children) on a signed-in operator's session",
			rr.Code, fake.enabled)
	}
}

// redReplayQueue is a queue.Browsable + queue.Replayable stand-in that records
// replays instead of re-firing real jobs.
type redReplayQueue struct{ replayed []string }

func (q *redReplayQueue) ListJobs(context.Context, string, int) ([]queue.Job, error) { return nil, nil }
func (q *redReplayQueue) Stats(context.Context) (queue.JobStats, error)              { return queue.JobStats{}, nil }
func (q *redReplayQueue) Replay(_ context.Context, id string) error {
	q.replayed = append(q.replayed, id)
	return nil
}

func TestQueueReplayRedEnforcesCSRF(t *testing.T) {
	q := &redReplayQueue{}
	h := mountAdmin(t, Config{Queue: q})

	// Positive control: same-origin replay re-queues the dead job.
	if rr := redFormPost(h, nil, "/admin/queue/_replay/job-1", url.Values{}, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: same-origin replay got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	if !slices.Contains(q.replayed, "job-1") {
		t.Fatalf("setup: same-origin replay not applied (%v) — harness broken, not the seam", q.replayed)
	}

	// The attack: cross-origin same-site POST re-fires a dead-lettered job.
	rr := redFormPost(h, nil, "/admin/queue/_replay/job-2", url.Values{}, true)
	if !redRefused(rr) || slices.Contains(q.replayed, "job-2") {
		t.Errorf("SECURITY: [admin-csrf-queue] cross-site POST /admin/queue/_replay/job-2 (Sec-Fetch-Site: same-site, Origin: evil.example.com, no _csrf) returned %d and replayed=%v — "+
			"handleQueueReplay verifies neither token nor origin (its doc comment leans on 'the form carries the CSRF token', which renders empty without the optional middleware), "+
			"so a forged urlencoded POST re-fires dead-lettered jobs on a signed-in operator's session",
			rr.Code, q.replayed)
	}
}
