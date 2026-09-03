//go:build red

package uihost

// RED TESTS — open findings, 2026-09-02 adversarial pass round 2
// (tests-only; no fix applied). One header block per finding.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Finding: 308 pattern-redirect Location carries C0/DEL ──────────
//
// Property: C0/DEL control bytes never reach outbound header values —
// the property core/handler/respond.go::sanitizeHeaderValue pins for its
// surfaces and round 1 pinned for the agent Link header.
// Surfaces: framework/uihost/uihost.go:handlePage (ResolveRedirect →
// http.Redirect 308), core-ui/app/redirect.go::substituteRedirect /
// safeResolvedTarget.
// Finding: substituteRedirect byte-substitutes request-path params into
// the target with no control-byte check, and safeResolvedTarget only
// rejects "//", "\", "://". A GET of /old/a%01b%7fc decodes to raw
// \x01/\x7f in r.URL.Path, which flows verbatim into the Location
// header (net/http's http.Redirect only hex-escapes non-ASCII; C0 and
// DEL pass raw, same as the round-1 Link finding). The partial branch
// of the same value is guarded (isSafePartialRedirect rejects control
// bytes, safe_path.go:31); this is the full-page 308 branch.
// Severity: production-facing (anonymous request-derived bytes in a
// response header).
// Fix direction: extend safeResolvedTarget (or the emit site) with the
// sanitizeHeaderValue contract — strip C0 except TAB and DEL — so a
// substituted target carrying control bytes fails closed, mirroring the
// isSafePartialRedirect control-byte clause.
func TestUihostRedRedirect308StripsControlBytes(t *testing.T) {
	a := app.NewApp("redir-ctl-red")
	a.Register("/", &testHomeComp{}, nil)
	a.RedirectPattern("/old/{rest...}", "/new/{rest...}")
	a.Register("/new/{rest...}", &paramJSONComp{}, nil)
	ds := New(a)

	req := httptest.NewRequest(http.MethodGet, "/old/a%01b%7fc", nil)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusPermanentRedirect || loc == "" {
		t.Fatalf("setup: expected 308 + Location from the pattern redirect, got %d %q (body %.200s)",
			rec.Code, loc, rec.Body.String())
	}
	for i := range len(loc) {
		c := loc[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			t.Errorf("SECURITY: [uihost] 308 Location %q carries raw control byte 0x%02x at %d — "+
				"percent-decoded request-path params flow through substituteRedirect into the "+
				"Location header with no C0/DEL check (safeResolvedTarget rejects //, \\, :// only); "+
				"the partial branch of the same value IS guarded (isSafePartialRedirect)", loc, c, i)
			break
		}
	}
}

// ─── Finding: X-Forwarded-Proto scheme reflected verbatim ────────────
//
// Property: absolute discovery URLs (Link header, agent card) never
// carry a scheme derived verbatim from the client-settable
// X-Forwarded-Proto header.
// Surfaces: framework/uihost/agentready.go:resolveBaseURL (:702-705) →
// writeAgentLinkHeaders (Link header on every HTML page) and
// handleAgentCard → buildAgentCard (supportedInterfaces url).
// Finding: resolveBaseURL refuses X-Forwarded-Host one comment-block
// earlier (cache-poisoning rationale) but trusts X-Forwarded-Proto
// verbatim: `scheme = u` with no value check. Every other consumer in
// the repo compares the header (EqualFold "https" — core/middleware
// csrf.go/security.go, battery/auth, examples); only agentready
// reflects it into absolute URLs. A request with
// X-Forwarded-Proto: javascript makes every Link target and the card's
// JSON-RPC URL javascript://host/…, poisoned into any shared cache that
// fronts the host.
// Severity: production-facing (cache poisoning of discovery URLs).
// Fix direction: accept XFP only when EqualFold "http"/"https",
// otherwise fall back to the TLS-derived scheme — same shape as
// core/middleware's XFP checks.

// xfpRedUIHost builds the agent-ready host the two X-Forwarded-Proto tests
// drive (same construction the merged test used).
func xfpRedUIHost(t *testing.T) *UIHost {
	t.Helper()
	a := app.NewApp("xfp-red")
	a.Register("/", &testHomeComp{}, nil)
	return New(a,
		WithAgentReady(AgentReadyConfig{
			Title:   "T",
			Summary: "s",
			AgentCard: &AgentCardConfig{
				Name:        "T",
				Description: "d",
				MCPEndpoint: "/mcp",
			},
		}),
	)
}

// xfpAssertSchemeOK fails when a discovery URL carries a scheme reflected
// verbatim from the client-settable X-Forwarded-Proto header.
func xfpAssertSchemeOK(t *testing.T, label, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Errorf("%s: unparseable URL %q: %v", label, raw, err)
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https":
	default:
		t.Errorf("SECURITY: [uihost] %s %q carries scheme %q reflected verbatim from the "+
			"client-settable X-Forwarded-Proto header (agentready.go:702-705); X-Forwarded-Host "+
			"is refused one block earlier for exactly this cache-poisoning reason, and every "+
			"other XFP consumer in the repo only compares EqualFold \"https\"",
			label, raw, u.Scheme)
	}
}

// TestUihostRedForwardedProtoLinkURLs: the Link header on every HTML page.
func TestUihostRedForwardedProtoLinkURLs(t *testing.T) {
	ds := xfpRedUIHost(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "javascript")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	link := rec.Header().Get("Link")
	if link == "" {
		t.Fatalf("setup: no Link header emitted — bundle linkHeaders default changed?")
	}
	for _, part := range strings.Split(link, ", ") {
		open := strings.Index(part, "<")
		closing := strings.Index(part, ">")
		if open < 0 || closing <= open {
			continue
		}
		xfpAssertSchemeOK(t, "Link header URL", part[open+1:closing])
	}
}

// TestUihostRedForwardedProtoAgentCard: the agent card's
// supportedInterfaces url.
func TestUihostRedForwardedProtoAgentCard(t *testing.T) {
	ds := xfpRedUIHost(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	req.Header.Set("X-Forwarded-Proto", "javascript")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: agent card status %d, body %.200s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("setup: agent card JSON: %v", err)
	}
	ifaces, ok := doc["supportedInterfaces"].([]any)
	if !ok || len(ifaces) == 0 {
		t.Fatalf("setup: no supportedInterfaces in card: %v", doc["supportedInterfaces"])
	}
	iface, ok := ifaces[0].(map[string]any)
	if !ok {
		t.Fatalf("setup: supportedInterfaces[0] is %T, want object", ifaces[0])
	}
	raw, _ := iface["url"].(string)
	if raw == "" {
		t.Fatalf("setup: supportedInterfaces[0].url empty")
	}
	xfpAssertSchemeOK(t, "agent card URL", raw)
}

// uihostRedServeExpectNoPanic drives GET path against ds and fails when a
// screen component panic escapes ServeHTTP.
func uihostRedServeExpectNoPanic(t *testing.T, ds *UIHost, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	escaped := any(nil)
	func() {
		defer func() { escaped = recover() }()
		ds.ServeHTTP(rec, req)
	}()
	if escaped != nil {
		t.Errorf("SECURITY/ROBUSTNESS: [uihost] screen component panic %v escaped ServeHTTP "+
			"for GET %s — serveNotFound (:1967) / PWAOfflineHTML (:372) call Render() raw, "+
			"bypassing the SafeRenderCtx containment every other render path applies; the "+
			"standalone host wires no recovery middleware, so the request dies with no response",
			escaped, path)
	}
}

// TestUihostRedNotFoundRenderSafe: the configured not-found screen's Render()
// is called raw by serveNotFound (:1967).
func TestUihostRedNotFoundRenderSafe(t *testing.T) {
	a := app.NewApp("nf-panic-red")
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a, WithNotFoundScreen(panicNotFoundScreen{}))
	uihostRedServeExpectNoPanic(t, ds, "/definitely-not-a-route-red")
}

// TestUihostRedPWARenderSafe: the PWA offline screen's Render() is called raw
// by PWAOfflineHTML (:372).
func TestUihostRedPWARenderSafe(t *testing.T) {
	a := app.NewApp("pwa-panic-red")
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{Name: "P", OfflineScreen: panicNotFoundScreen{}}))
	uihostRedServeExpectNoPanic(t, ds, "/__gofastr/pwa/offline")
}

// panicNotFoundScreen is a screen component whose render always panics.
type panicNotFoundScreen struct{}

func (panicNotFoundScreen) Render() render.HTML { panic("red-test: screen render boom") }

// ─── Finding: server-action body accepts duplicate/case-folded keys ──
//
// Property: mutating JSON endpoints reject duplicate and case-folded
// top-level keys — the contract core/handler/bind.go::validateBodyKeys
// pins for the typed-handler surfaces (last-wins decoding lets a
// proxy/attacker-crafted body smuggle a different value past a reader
// that only saw one spelling of the key).
// Surfaces: framework/uihost/uihost.go:handleServerAction (:2458-2466,
// decodeBounded plain json.Decode of {action,params,session,
// componentId}).
// Finding: encoding/json resolves duplicate and case-folded keys
// last-wins, so {"action":"safe","action":"danger",…} decodes clean
// and invokes the SECOND action. Any intermediary that inspects only
// the first spelling (logging, a future signature over the body) is
// desynchronised from the value the server executes.
// Severity: production-facing (unauthenticated body reaches the
// parser; exploitation needs a second reader, but the parity gap with
// core/handler is the finding).
// Fix direction: run validateBodyKeys over the body in decodeBounded
// (or before Decode here) and answer its existing 400 "invalid JSON
// body", matching the typed-handler surfaces.
// Round-6 mechanism split: the exact-duplicate and case-folded shapes are
// separate top-level tests below (independently fixable mechanisms).
// redActionFlags records which of the two compiled actions ran.
type redActionFlags struct{ safe, danger bool }

// redActionHarness compiles a two-action screen ("safe"/"danger"), proves
// the single-key form executes (so a later failure is about the key shape,
// not the harness), and returns a raw-body POST func, the compiled action
// id, and the ran flags.
func redActionHarness(t *testing.T) (post func(string) *httptest.ResponseRecorder, id string, ran *redActionFlags) {
	t.Helper()
	ran = &redActionFlags{}
	comp := &actionTestComp{html: "<p>dup</p>", actions: func() {
		redAction("safe", func() { ran.safe = true })
		redAction("danger", func() { ran.danger = true })
	}}
	a := app.NewApp("dup-action-keys-red")
	a.RegisterScreen(app.NewScreen("/dup", comp).WithTitle("Dup"), nil)
	ds := New(a)
	ds.AutoCompileActions()

	id = ""
	ds.mu.RLock()
	for k := range ds.actionHandlers {
		if id == "" || k == "dup" {
			id = k
		}
	}
	ds.mu.RUnlock()
	if id == "" {
		t.Fatal("setup: no server actions compiled for the screen")
	}

	sess := ds.CreateSession()
	post = func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		return rec
	}

	// Sanity: the endpoint accepts and executes the single-key form, so
	// any later failure is about the duplicate keys, not the harness.
	if rec := post(`{"action":"safe","params":{},"componentId":"` + id + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("setup: single-key action POST = %d (body %.200s)", rec.Code, rec.Body.String())
	}
	if !ran.safe {
		t.Fatal("setup: single-key POST did not invoke the safe action")
	}
	return post, id, ran
}

// TestUihostRedActionRejectsDuplicateKeys: exact duplicate "action" keys —
// wire-level last-wins. {"action":"safe","action":"danger",…} decodes
// clean and invokes the SECOND action.
func TestUihostRedActionRejectsDuplicateKeys(t *testing.T) {
	post, id, ran := redActionHarness(t)
	body := `{"action":"safe","action":"danger","params":{},"componentId":"` + id + `"}`
	if rec := post(body); rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/action accepted %s with status %d — "+
			"encoding/json resolved the duplicate top-level key last-wins instead "+
			"of rejecting the body (validateBodyKeys parity, core/handler/bind.go:136)",
			body, rec.Code)
	}
	if ran.danger {
		t.Errorf("SECURITY: [uihost] the last-wins decode EXECUTED the second \"action\" value " +
			"(\"danger\") — a body with duplicate top-level keys must be rejected before decode")
	}
}

// TestUihostRedActionRejectsCaseFoldedKeys: "action"/"Action" fold onto
// the same struct field via stdlib json's tag-insensitive match — a
// duplicate modulo folding; survives a dedup-only fix.
func TestUihostRedActionRejectsCaseFoldedKeys(t *testing.T) {
	post, id, ran := redActionHarness(t)
	body := `{"action":"safe","Action":"danger","params":{},"componentId":"` + id + `"}`
	if rec := post(body); rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/action accepted %s with status %d — "+
			"encoding/json resolved the case-folded top-level key onto the same field last-wins instead "+
			"of rejecting the body (validateBodyKeys parity, core/handler/bind.go:136)",
			body, rec.Code)
	}
	if ran.danger {
		t.Errorf("SECURITY: [uihost] the last-wins decode EXECUTED the second \"action\" value " +
			"(\"danger\") — a body with case-folded top-level keys must be rejected before decode")
	}
}

// ─── Finding: embed exchange body accepts duplicate keys ────────────
//
// Property: same strict-keys contract as above, on the HMAC handshake.
// Surfaces: framework/uihost/embed.go:handleEmbedExchange (:536-540,
// plain json.NewDecoder(MaxBytesReader).Decode of {token,origin}; the
// 8KB cap is already there).
// Finding: duplicate/case-folded "token" or "origin" keys decode
// last-wins before Exchange HMAC-verifies, so a body whose first
// spelling says one nonce and whose second says another is silently
// normalized to the last value instead of being refused as malformed.
// Severity: production-facing (unauthenticated POST surface); same
// second-reader caveat as the action endpoint.
// Fix direction: validateBodyKeys before Decode; failures take the
// existing embedError 400 "malformed body" arm, keeping the
// indistinguishable-refusal property for everything downstream.
// Round-6 mechanism split: the exact-duplicate and case-folded shapes are
// separate top-level tests below (independently fixable mechanisms).
// TestUihostRedEmbedRejectsDuplicateKeys: exact duplicate "token" keys —
// wire-level last-wins ahead of the HMAC exchange.
func TestUihostRedEmbedRejectsDuplicateKeys(t *testing.T) {
	f := newEmbedFixture(t)
	body := `{"token":"first-token","token":"second-token","origin":"` + embedTestOrigin + `"}`
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/embed-exchange accepted %s with status %d — "+
			"duplicate top-level keys decode last-wins ahead of the HMAC exchange "+
			"instead of being rejected (validateBodyKeys parity)",
			body, rec.Code)
	}
}

// TestUihostRedEmbedRejectsCaseFoldedKeys: "Token"/"token" and
// "Origin"/"origin" fold onto the tagged fields via stdlib json's
// tag-insensitive match — duplicates modulo folding; survive a dedup-only
// fix.
func TestUihostRedEmbedRejectsCaseFoldedKeys(t *testing.T) {
	f := newEmbedFixture(t)
	for _, body := range []string{
		`{"Token":"first-token","token":"second-token","origin":"` + embedTestOrigin + `"}`,
		`{"token":"first-token","origin":"` + embedTestOrigin + `","Origin":"https://evil.example"}`,
	} {
		rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("SECURITY: [uihost] POST /__gofastr/embed-exchange accepted %s with status %d — "+
				"case-folded top-level keys decode last-wins ahead of the HMAC exchange "+
				"instead of being rejected (validateBodyKeys parity)",
				body, rec.Code)
		}
	}
}

// ─── Finding: partial-page early exits skip the no-store invariant ──
//
// Property: partial responses are never cacheable by shared caches —
// the threat model handlePartialPage's own comments state at :2017-2021
// and :2145-2149, and the re-mint arm already pins
// (uihost_test.go:313-318). Every early exit below returns BEFORE the
// unconditional Cache-Control: no-store at :2150, and only the re-mint
// path (:2022) sets it earlier.
// Surfaces: framework/uihost/uihost.go:handlePartialPage — the prefetch
// 204 (:2010-2013), the DecisionRedirect branches (:2100-2106),
// DecisionBlock (:2112), and the render-error 404 (:2080).
// Finding: two deterministic shapes ship with NO cache suppression:
//   (a) no cookie + X-Gofastr-Navigate:1 + X-Gofastr-Prefetch:1 → 204
//       with no Cache-Control and no Vary. Anonymous-reachable, gated
//       on a client-chosen header; a shared cache configured to store
//       no-freshness responses (nginx proxy_cache_valid, CDN
//       cache-everything) keys the empty 204 on the URL and blanks any
//       app route for later visitors.
//   (b) a LIVE session (re-mint block skipped, so :2022 never runs) +
//       a policy DecisionRedirect with an unsafe URL → hard 303, again
//       no Cache-Control; a cached 303 replays its Location onto later
//       visitors' navigations of that URL.
// Severity: production-facing behind an operator-configured shared
// cache that stores responses lacking freshness directives —
// non-default but the same in-model attacker the surrounding framework
// code documents (the embed Vary work, battery/cache, these very
// comments). If the maintainer rejects that cache model, this degrades
// to a consistency pin: the branches violate the function's own
// documented no-store invariant.
// Fix direction: set Cache-Control: no-store (plus Vary: Cookie) before
// every early return in handlePartialPage — or once at the top of the
// function — so no partial-shaped response can leave uncacheable-flagged.

// TestPartialPageRedNoStoreOnEarlyExit: both early-exit shapes (204
// prefetch with dead session; 303 policy redirect with live session)
// must carry Cache-Control: no-store like every other partial response.
func TestPartialPageRedNoStoreOnEarlyExit(t *testing.T) {
	// (a) Dead-session prefetch: 204 returns at :2012 before any
	// no-store is set.
	{
		ds := actionsHost()
		req := httptest.NewRequest("GET", "/act", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-Prefetch", "1")
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("prefetch with dead session: status %d, want 204", w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [uihost] prefetch 204 Cache-Control = %q, want no-store — "+
				"handlePartialPage returns at :2012 before the unconditional no-store at :2150, "+
				"so an anonymous, header-gated empty 204 ships with no cache suppression and a "+
				"shared cache configured to store no-freshness responses can blank any app route "+
				"for later visitors",
				cc)
		}
	}

	// (b) Live session + policy redirect with an unsafe URL: the re-mint
	// block is skipped (session already live, :2022 never runs) and the
	// 303 arm at :2105 returns before :2150.
	{
		pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Redirect("https://evil.example/login")
		})
		application := app.NewApp("t")
		application.RegisterScreen(
			app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol),
			nil,
		)
		ds := New(application)

		sess := ds.CreateSession()
		req := httptest.NewRequest("GET", "/dash", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("policy redirect partial: status %d, want 303 (unsafe redirect URL falls back to a hard redirect)", w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [uihost] partial 303 Cache-Control = %q, want no-store — "+
				"with a live session the re-mint no-store at :2022 never runs and the 303 arm "+
				"at :2105 returns before the unconditional one at :2150, so the redirect ships "+
				"with no cache suppression and a shared cache can replay its Location onto "+
				"later visitors' navigations",
				cc)
		}
	}
}
