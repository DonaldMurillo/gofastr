//go:build red

package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// sfsRedReq names the browser inputs crossSite reads. Empty origin/sfs
// omits the header (non-browser transport); empty host keeps the
// loopback server host.
type sfsRedReq struct {
	host   string
	origin string
	sfs    string
}

// sfsRedDo drives the mounted CSRF test world (l.ServeHTTP, both the
// Server.Mount and MountPanel surfaces) with full control over Host,
// Origin, and Sec-Fetch-Site — exactly the three inputs crossSite
// reads. POSTs are no-preflight text/plain, the same browser shape
// csrfPost documents.
func sfsRedDo(t *testing.T, l *live.Live, method, path, body string, rq sfsRedReq) *httptest.ResponseRecorder {
	t.Helper()
	if rq.host == "" {
		rq.host = "localhost:8765"
	}
	req := httptest.NewRequest(method, "http://"+rq.host+path, strings.NewReader(body))
	req.Host = rq.host
	if rq.origin != "" {
		req.Header.Set("Origin", rq.origin)
	}
	if rq.sfs != "" {
		req.Header.Set("Sec-Fetch-Site", rq.sfs)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain")
	}
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	return rec
}

// proposeRedPlan seeds one pending destructive plan, the card a human
// is supposed to eyeball before approve_plan runs.
func proposeRedPlan(t *testing.T, tools *protocol.Tools, id string) {
	t.Helper()
	if res := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID:  id,
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("setup broken: propose_plan %s returned %+v — the plan must be pending for the approve legs to run", id, res)
	}
}

// RED TEST — open finding, 2026-09-06/07 adversarial round 5 (fringe wave; tier T2).
// Property: a "same-site" Sec-Fetch-Site value is not proof of same-origin.
// The site computation excludes ports, so a page on ANY sibling port of the
// kiln host is same-site while its Origin still names a different origin.
// The repo convention (pinned: battery/auth core.go rejectCrossSiteForm →
// TestLogoutRejectsSameSiteSiblingOrigin; battery/admin csrf.go →
// TestRbacGrantRefusesCrossSitePost) treats "same-site" as unproven and falls
// through to the Origin↔Host comparison, which includes ports and refuses.
// battery/setup's copy is this round's sibling red arm
// (TestSetupRedSameSiteOriginRefused); this is the kiln library copy.
// Surfaces: kiln/chat/csrf.go::crossSite (:85-90) — the switch trusts
// "same-site" BEFORE any Origin compare — via sameOriginOnly (:27-35) over
// Server.Mount's POST routes (/kiln/tool/{name} incl. approve_plan,
// /kiln/chat/message) and MountPanel's five RPC routes (panel.go :89-93).
// Finding: a page at http://localhost:3000 (sibling port of the kiln bind
// localhost:8765) POSTs approve_plan with Sec-Fetch-Site: same-site and
// Origin: http://localhost:3000; crossSite returns false at the switch, the
// Origin compare never runs, and the destructive plan is approved without the
// human seeing the card. Recon executed 200/Approved=true on both routes.
// The shipped binary is covered only by cmd/kiln's outer originGuard;
// csrf.go's own doc (:52) pins "Mount is a library surface", so the guard
// must hold unwrapped.
// Fix direction: drop "same-site" from crossSite's trusted case and let it
// fall through to the Origin↔Host comparison ("same-origin"/"none" stay
// trusted, "cross-site" stays refused), matching rejectCrossSiteForm.
func TestCSRFRedSameSiteFallsThrough(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	for _, id := range []string{"ss-tool", "ss-panel", "ss-agent", "ss-self"} {
		proposeRedPlan(t, tools, id)
	}

	// Attack legs: browser-accurate headers for a page served from a
	// sibling PORT of the panel's own host. Sec-Fetch-Site: same-site is
	// the honest browser value (ports don't count for site computation);
	// the Origin compare that would catch the port difference never runs.
	legs := []struct{ name, path, planID string }{
		{"tool route", "/kiln/tool/approve_plan", "ss-tool"},
		{"panel route", "/kiln/panel/approve_plan", "ss-panel"},
	}
	for _, leg := range legs {
		rec := sfsRedDo(t, l, http.MethodPost, leg.path,
			`{"plan_id":"`+leg.planID+`"}`,
			sfsRedReq{host: "localhost:8765", origin: "http://localhost:3000", sfs: "same-site"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY: [kiln-csrf-samesite] %s accepted the sibling-port approve (Sec-Fetch-Site: same-site, Origin: http://localhost:3000, Host: localhost:8765): status %d, body %.200s — crossSite trusts \"same-site\" before the Origin↔Host compare, and same-site excludes ports, so a page on any sibling port of the kiln host drives every state-changing route; battery/auth rejectCrossSiteForm (TestLogoutRejectsSameSiteSiblingOrigin) and battery/admin (TestRbacGrantRefusesCrossSitePost) pin the fall-through convention this guard diverges from",
				leg.name, rec.Code, rec.Body.String())
		}
		if planApproved(l, leg.planID) {
			t.Errorf("SECURITY: [kiln-csrf-samesite] sibling-port POST approved plan %s on the %s (status %d, body %.200s) — the plan gate's human approval was bypassed by a page the Origin compare would have refused",
				leg.planID, leg.name, rec.Code, rec.Body.String())
		}
	}

	// Controls: green today AND after the fall-through fix — they pin the
	// caller classes the guard must keep refusing / serving.
	if rec := csrfPost(t, l, "/kiln/tool/approve_plan", `{"plan_id":"ss-ctl-pin"}`); rec.Code != http.StatusForbidden {
		t.Errorf("control broken: no-Fetch-Metadata cross-origin POST (Origin https://evil.example) got %d, want 403 — TestPOSTRoutesRejectCrossOrigin's pin must hold in this world", rec.Code)
	}
	rec := sfsRedDo(t, l, http.MethodPost, "/kiln/tool/approve_plan", `{"plan_id":"ss-agent"}`, sfsRedReq{})
	if rec.Code != http.StatusOK || !planApproved(l, "ss-agent") {
		t.Errorf("control broken: no-Origin POST (agent transport, plan_gate_test.go's contract) got %d approved=%v, want 200/true: %.200s",
			rec.Code, planApproved(l, "ss-agent"), rec.Body.String())
	}
	rec = sfsRedDo(t, l, http.MethodPost, "/kiln/tool/approve_plan", `{"plan_id":"ss-self"}`,
		sfsRedReq{origin: "http://localhost:8765", sfs: "same-origin"})
	if rec.Code != http.StatusOK || !planApproved(l, "ss-self") {
		t.Errorf("control broken: same-origin POST (operator panel) got %d approved=%v, want 200/true: %.200s",
			rec.Code, planApproved(l, "ss-self"), rec.Body.String())
	}
}

// RED TEST — open finding, 2026-09-06/07 adversarial round 5 (fringe wave; tier T3).
// CONTRACT-QUESTION: the shipped cmd/kiln binary wraps this surface in an
// outer originGuard that pins the Host, so there is no shipped-binary
// exploit; the claim is library-level. csrf.go's own doc (:52) pins "Mount is
// a library surface" (unwrapped mounting is supported), and readGuard 25
// lines below applies this exact pin for the same stated reason. This
// asserts the write guard's self-sufficiency and intra-file consistency.
// Property: the POST guard applies the loopback Host pin its read sibling
// applies. DNS rebinding arrives same-origin: after the rebind the
// attacker's page and the listener agree on the attacker-named Host, so
// every Origin↔Host comparison passes and only a Host pin refuses it —
// readGuard's own words (:41-44, "Only a Host pin refuses it"), pinned by
// TestReadRoutesRefuseCrossSiteSub's "rebound Host with matching Origin"
// case.
// Surfaces: kiln/chat/csrf.go::sameOriginOnly (:27-35) — checks crossSite
// only, never r.Host — over the Server.Mount POST routes and MountPanel's
// five RPC routes.
// Finding: a rebound page POSTs /kiln/tool/approve_plan with Host+Origin
// evil.test:8765 and Sec-Fetch-Site: same-origin (honest browser values
// post-rebind); sameOriginOnly waves it through and the destructive plan is
// approved, while the read arm refuses the identical request on /kiln/world.
// Recon executed 200/Approved on the POST beside a 403 on the GET.
// Fix direction: sameOriginOnly gains readGuard's second check — Origin
// present + non-loopback r.Host → 403 — keeping no-Origin callers (agent
// transport, curl) untouched, exactly as readGuard does.
func TestCSRFRedWriteGuardHostPinned(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	for _, id := range []string{"hb-tool", "hb-agent"} {
		proposeRedPlan(t, tools, id)
	}

	// Attack leg: DNS-rebinding shape. Host and Origin agree on the
	// attacker's name and Sec-Fetch-Site is honestly "same-origin", so
	// only a Host pin can refuse it — the pin the write guard lacks.
	rec := sfsRedDo(t, l, http.MethodPost, "/kiln/tool/approve_plan", `{"plan_id":"hb-tool"}`,
		sfsRedReq{host: "evil.test:8765", origin: "http://evil.test:8765", sfs: "same-origin"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("SECURITY: [kiln-csrf-write-hostpin] rebind-Host approve_plan (Host+Origin evil.test:8765, Sec-Fetch-Site: same-origin) got status %d, body %.200s — sameOriginOnly never checks r.Host while readGuard 25 lines below refuses this exact request (\"Only a Host pin refuses it\", TestReadRoutesRefuseCrossSiteSub), so a rebound page silently approves the destructive plan the read arm won't even let it see",
			rec.Code, rec.Body.String())
	}
	if planApproved(l, "hb-tool") {
		t.Errorf("SECURITY: [kiln-csrf-write-hostpin] rebind-Host POST approved plan hb-tool (status %d, body %.200s) — the write guard admits the DNS-rebinding caller its read sibling refuses; the plan gate's human approval was bypassed",
			rec.Code, rec.Body.String())
	}

	// Controls: the read arm's pin holds (green today and post-fix), and
	// the agent transport keeps working.
	if rec := sfsRedDo(t, l, http.MethodGet, "/kiln/world", "",
		sfsRedReq{host: "evil.test:8765", origin: "http://evil.test:8765", sfs: "same-origin"}); rec.Code != http.StatusForbidden {
		t.Errorf("control broken: rebind-Host GET /kiln/world got %d, want 403 — TestReadRoutesRefuseCrossSiteSub's Host pin must hold", rec.Code)
	}
	rec = sfsRedDo(t, l, http.MethodPost, "/kiln/tool/approve_plan", `{"plan_id":"hb-agent"}`, sfsRedReq{})
	if rec.Code != http.StatusOK || !planApproved(l, "hb-agent") {
		t.Errorf("control broken: loopback no-Origin POST (agent transport) got %d approved=%v, want 200/true: %.200s",
			rec.Code, planApproved(l, "hb-agent"), rec.Body.String())
	}
}
