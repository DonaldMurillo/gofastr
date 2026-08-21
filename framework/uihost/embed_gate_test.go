package uihost

import (
	"context"
	"encoding/json"
	html "html"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// The frame's only credential is the grant header. Every /__gofastr/* endpoint
// an embedded surface needs has to accept it, because a frame never has a
// cookie and never will, and the ones that don't fail silently rather than
// loudly, which is why this file exists.

func TestContentRenderExposesTheGrant(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	body := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", grant)
	}).Body.String()

	// Not merely "some grant": the surface and scopes have to survive, or a
	// screen gating on HasScope cannot tell a read-only embed from a full one.
	if !strings.Contains(body, "grant:present/reports/read") {
		t.Fatalf("the screen could not see the grant that authorised its own render.\n"+
			"A screen doing `if !embedded { firstPartyControls() }` renders those controls\n"+
			"inside the customer's iframe.\ngot: %s", body)
	}
}

func TestFirstPartyRenderHasNoGrant(t *testing.T) {
	// The other half of the contract: ok=false must still mean "not embedded",
	// or the check above is worthless in the direction that matters.
	f := newEmbedFixture(t)
	body := f.do(t, http.MethodGet, "/reports", "").Body.String()
	if !strings.Contains(body, "grant:absent") {
		t.Fatalf("an ordinary first-party render reported a grant:\n%s", body)
	}
}

func TestEmbedRuntimeCarriesActions(t *testing.T) {
	f := newEmbedFixture(t)
	js := f.do(t, http.MethodGet, "/__gofastr/embed-runtime.js", "").Body.String()

	// Assert the property, not the byte-identical bundle: GetActionJS
	// recompiles from a map, so a whole-string comparison depends on iteration
	// order and fails at random.
	if f.host.GetActionJS() == "" {
		t.Fatal("the fixture compiled no action JS, so this test could not fail " +
			"whatever the runtime endpoint served")
	}
	if !strings.Contains(js, "G.register(") || !strings.Contains(js, "embed-probe") {
		t.Fatalf("the embed runtime does not carry the app's compiled actions.\n"+
			"Nothing else calls __gofastr.register, so handlers stays empty for the\n"+
			"life of the frame: data-action-mount nodes never fill, and data-action\n"+
			"clicks are preventDefault()ed and then dropped.\ntail: %s",
			js[max(0, len(js)-200):])
	}
}

func TestActionsEndpointsAcceptAGrant(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	withGrant := func(r *http.Request) { r.Header.Set("X-Gofastr-Embed", grant) }

	// Assert the success, not the absence of one specific failure: "not 401"
	// was satisfied by 403, 404, 500 and 503 alike.
	rec := f.do(t, http.MethodGet, "/__gofastr/actions.js", "", withGrant)
	if rec.Code != http.StatusOK {
		t.Errorf("/__gofastr/actions.js answered a valid grant with %d, want 200", rec.Code)
	} else if !strings.Contains(rec.Body.String(), "G.register(") {
		t.Errorf("/__gofastr/actions.js served a grant something that is not the action bundle:\n%s",
			rec.Body.String())
	}
	// And still refuses a caller with neither credential.
	if got := f.do(t, http.MethodGet, "/__gofastr/actions.js", "").Code; got != http.StatusUnauthorized {
		t.Errorf("/__gofastr/actions.js served an anonymous caller: status %d", got)
	}
}

func TestGrantSatisfiesTheWidgetSessionCheck(t *testing.T) {
	// The /state and /chrome URLs are predictable, so scoping the CATALOG only
	// decides what a caller is TOLD about. The gate itself has to know which
	// surface the grant is for, or anyone with devtools on a legitimate customer
	// page reads the state of a widget scoped to /admin.
	only := func(path string) []widget.RouteMatcher {
		return []widget.RouteMatcher{func(p string) bool { return p == path }}
	}
	scratch := router.New()
	widget.Mount(scratch, &widget.Definition{Name: "r3gate-mine", Routes: only("/reports")})
	widget.Mount(scratch, &widget.Definition{Name: "r3gate-theirs", Routes: only("/admin")})

	f := newEmbedFixture(t)
	// grantFor serves a request, which lazily mounts the standalone router and
	// installs the predicate.
	grant := f.grantFor(t, "reports")

	check := widget.SessionCheck()
	if check == nil {
		t.Fatal("no session predicate installed")
	}
	withGrant := func(name string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/core-ui/widget/"+name+"/chrome", nil)
		req.Header.Set("X-Gofastr-Embed", grant)
		return req
	}

	if !check(withGrant("r3gate-mine")) {
		t.Error("a RequireSession widget on the grant's OWN surface refuses the frame's " +
			"only credential — dead DOM one hop past the catalog")
	}
	if check(withGrant("r3gate-theirs")) {
		t.Error("a grant for /reports opened a widget scoped to /admin; filtering the " +
			"catalog does not stop a direct request to a predictable URL")
	}
	if check(withGrant("r3gate-does-not-exist")) {
		t.Error("the predicate accepted an unregistered widget name")
	}
	if check(httptest.NewRequest(http.MethodGet, "/core-ui/widget/r3gate-mine/chrome", nil)) {
		t.Error("the predicate accepts a request carrying no credential at all")
	}
}

func TestWidgetCatalogIsScopedToTheGrantsSurface(t *testing.T) {
	// Registration is process-global and the package exposes no reset, so these
	// names are deliberately unique to this test. Nothing else in the package
	// asserts on catalog contents.
	only := func(path string) []widget.RouteMatcher {
		return []widget.RouteMatcher{func(p string) bool { return p == path }}
	}
	scratch := router.New()
	widget.Mount(scratch, &widget.Definition{Name: "r2gate-reports", Routes: only("/reports")})
	widget.Mount(scratch, &widget.Definition{Name: "r2gate-admin", Routes: only("/admin")})

	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	// The caller picks the page. A grant proves nothing about any page but the
	// one its own surface renders, so the query must not be believed.
	body := f.do(t, http.MethodGet, "/__gofastr/widgets?page=/admin", "", func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", grant)
	}).Body.String()

	if strings.Contains(body, "r2gate-admin") {
		t.Errorf("a grant for /reports read the /admin catalog:\n%s", body)
	}
	if !strings.Contains(body, "r2gate-reports") {
		t.Errorf("the grant's own surface was filtered out:\n%s", body)
	}

	// Omitting the parameter entirely must not fall back to the whole registry.
	unfiltered := f.do(t, http.MethodGet, "/__gofastr/widgets", "", func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", grant)
	}).Body.String()
	if strings.Contains(unfiltered, "r2gate-admin") {
		t.Errorf("a grant with no page parameter read the unfiltered registry:\n%s", unfiltered)
	}
}

func TestRejectedThemesLeaveNoBookkeeping(t *testing.T) {
	f := newEmbedFixture(t)

	// Each of these is unusable for a different reason, and every one of them
	// is reached AFTER the reservation is recorded.
	for _, bad := range []string{"!!!not-base64!!!", "bm90LWpzb24", "e30", strings.Repeat("A", 32<<10)} {
		f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+bad, "")
	}
	for i := 0; i < 200; i++ {
		f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme=zzz"+strings.Repeat("q", i), "")
	}

	f.host.embedThemes.mu.Lock()
	resolved := len(f.host.embedThemes.resolved["reports"])
	used := len(f.host.embedThemes.used["reports"])
	f.host.embedThemes.mu.Unlock()

	if resolved != 0 || used != 0 {
		t.Fatalf("rejected theme parameters were retained: resolved=%d used=%d, want 0/0.\n"+
			"The shell route is unauthenticated, so anything retained per rejected\n"+
			"parameter grows without bound while every cap and count reads healthy.",
			resolved, used)
	}
}

// An oversize parameter must be refused before it can take a reservation.
//
// Asserting on the maps after the request is not enough: release cleans them
// either way, so that version of this test passed with the bound in the wrong
// place. What the placement actually decides is whether garbage can consume a
// slot, and with a cap of 2, consuming a slot means EVICTING a customer's real
// branding. That is observable.
func TestOversizeThemeCannotEvictARealTheme(t *testing.T) {
	f := newEmbedFixture(t) // reports declares MaxVariants: 2
	first := embedThemeParam(t, map[string]string{"color-primary": "#111111"})
	second := embedThemeParam(t, map[string]string{"color-primary": "#222222"})

	render := func(param string) {
		t.Helper()
		body := f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+param, "").Body.String()
		if !strings.Contains(body, "app.css?t=") {
			t.Fatalf("surface rendered unthemed for a valid parameter:\n%s", body)
		}
	}
	render(first)
	render(second)

	// The cap is now full. A parameter too large to be usable must not take a
	// slot. Taking one means evicting a customer's real branding.
	//
	// The eviction is not observable through the served key: keys are content
	// addresses, so a variant that is evicted and then re-registered comes back
	// under the same name. The reservation table is where it shows.
	f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+strings.Repeat("A", 64<<10), "")

	f.host.embedThemes.mu.Lock()
	held := make([]string, 0, 2)
	for param := range f.host.embedThemes.resolved["reports"] {
		held = append(held, param)
	}
	f.host.embedThemes.mu.Unlock()

	if len(held) != 2 {
		t.Fatalf("holding %d variants after an oversize parameter, want the 2 real ones — "+
			"garbage took a slot and evicted live branding", len(held))
	}
	for _, want := range []string{first, second} {
		if !slices.Contains(held, want) {
			t.Errorf("a real theme was evicted by an oversize parameter")
		}
	}
}

// Regression: the response body carries expires_in_ms, which is derived from
// the wall clock at write time. Comparing whole bodies made this test fail
// roughly once in a thousand runs with the message "one nonce bought two
// identities", sending anyone who read it after a single-use violation that
// had not happened. The property is about the GRANT.
func TestExchangeIsIdempotentOnTheGrant(t *testing.T) {
	f := newEmbedFixture(t)
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})

	grantOf := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var out struct {
			Grant string `json:"grant"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out.Grant
	}

	first := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	// Cross a second boundary before replaying.
	//
	// MintGrant is deterministic in its claims and its two time fields are
	// truncated to whole seconds, so two mints inside one second are
	// byte-identical. Comparing the grants back to back therefore passed even
	// with the replay branch deleted and Exchange re-minting every time. The
	// equality came from the clock, not from the store. This sleep is what makes
	// the assertion load-bearing: a re-mint now lands in a different second and
	// produces different bytes.
	time.Sleep(1100 * time.Millisecond)
	second := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses %d / %d, want 200 / 200", first.Code, second.Code)
	}
	if grantOf(first) != grantOf(second) {
		t.Fatal("the second exchange returned a DIFFERENT grant — one nonce bought two identities")
	}
}

// A duplicate is not a refusal. Both used to be reported as the same false, so
// two visitors opening one customer's page at the same moment on a cold process
// had one of them rendered in the app palette.
func TestReserveDistinguishesDuplicateFromFull(t *testing.T) {
	var state embedThemeState

	if ok, _, dup := state.reserve("reports", "brand-a", 2); !ok || dup {
		t.Fatalf("first reserve: ok=%v dup=%v, want true/false", ok, dup)
	}
	// Same parameter, still in flight.
	if ok, _, dup := state.reserve("reports", "brand-a", 2); ok || !dup {
		t.Errorf("a second request for the SAME theme reported ok=%v dup=%v, want false/true", ok, dup)
	}
	// A different parameter with every slot in flight is a genuine refusal.
	state.reserve("reports", "brand-b", 2)
	if ok, _, dup := state.reserve("reports", "brand-c", 2); ok || dup {
		t.Errorf("a full cap reported ok=%v dup=%v, want false/false", ok, dup)
	}
}

// Asserting that the catalog STRING contains "?t=" is not enough, and the
// previous version of this test did exactly that. It passed while the property
// was fully broken: the runtime builds the real href by appending its own
// version parameter, and appending it with a second "?" folded the theme key
// into one unparseable value, so the server read an unknown key and served the
// app palette.
//
// So build the URL the way the runtime builds it, fetch it, and check the bytes.
//
// This covers the SERVER half only: it reimplements kernel.js's concatenation
// rather than executing it, so mutating the fragment cannot fail it. The client
// half is gated separately by TestKernelAppendsVersionWithTheRightSeparator in
// core-ui/runtime, which is where the bug actually lived. Neither test is
// sufficient alone and this comment exists so the pair is not mistaken for
// end-to-end coverage.
func TestEmbedComponentCSSFollowsTheCustomerTheme(t *testing.T) {
	// Reads the theme value DIRECTLY rather than emitting var(), which is the
	// whole class of component this fix exists for.
	registry.RegisterStyle("r3themeprobe", func(th style.Theme) string {
		return ".r3themeprobe{color:" + th.Colors.Primary.Value + "}"
	})

	f := newEmbedFixture(t)
	customer := "#0d9488"
	param := embedThemeParam(t, map[string]string{"color-primary": customer})
	shell := f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+param, "").Body.String()

	_, rest, ok := strings.Cut(shell, `id="gofastr-catalog"`)
	if !ok {
		t.Fatal("shell emitted no component catalog")
	}
	catalogJSON, _, _ := strings.Cut(rest, "</script>")
	_, catalogJSON, _ = strings.Cut(catalogJSON, ">")

	var catalog map[string]struct {
		StylePath string `json:"stylePath"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(catalogJSON)), &catalog); err != nil {
		t.Fatalf("catalog is not JSON: %v\n%s", err, catalogJSON)
	}
	entry, ok := catalog["r3themeprobe"]
	if !ok {
		t.Fatal("probe component missing from the catalog")
	}

	// Exactly what core-ui/runtime/frag/kernel.js does.
	href := entry.StylePath
	if entry.Version != "" {
		sep := "?"
		if strings.Contains(href, "?") {
			sep = "&"
		}
		href += sep + "v=" + entry.Version
	}

	css := f.do(t, http.MethodGet, href, "").Body.String()
	if !strings.Contains(css, customer) {
		t.Errorf("component CSS at %s did not render under the customer's palette.\n"+
			"app.css carries the customer's colour and this does not, so the embed "+
			"comes back half-rebranded.\ngot: %s", href, css)
	}
}

// A reservation is stored as an empty key, and reporting that as a hit made the
// caller render under the app theme, the very failure the duplicate branch was
// added to prevent. The window is the whole of resolveEmbedTheme, so a second
// request for the same theme almost always lands inside it.
func TestInFlightThemeIsNotAHit(t *testing.T) {
	var state embedThemeState
	if ok, _, _ := state.reserve("reports", "brand-a", 2); !ok {
		t.Fatal("first reserve was refused")
	}
	if key, found := state.lookup("reports", "brand-a"); found {
		t.Fatalf("lookup reported a hit for an in-flight reservation (key=%q). "+
			"The caller returns that key, which is empty, so the second visitor "+
			"renders in the app palette instead of the customer's.", key)
	}
	// Once recorded it is a real hit.
	state.record("reports", "brand-a", "variant-key")
	if key, found := state.lookup("reports", "brand-a"); !found || key != "variant-key" {
		t.Fatalf("lookup after record = %q, %v; want variant-key, true", key, found)
	}
}

// A presented credential's verdict is final. The runtime keeps sending an
// expired grant on purpose (so the server answers 401 rather than serving an
// anonymous render), and in a same-site framing the browser sends the viewer's
// session cookie alongside it, so falling back to the cookie answered the
// embed request as an entirely different, unrelated user.
func TestInvalidGrantDoesNotFallBackToASession(t *testing.T) {
	f := newEmbedFixture(t)

	// A REAL, valid session for this host. The point of the test is that a
	// working cookie must not rescue a refused grant. A bogus cookie would make
	// this pass on the cookie's own invalidity and prove nothing.
	sess := f.host.CreateSession()

	rec := f.do(t, http.MethodGet, "/__gofastr/actions.js", "", func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", "emg_not-a-real-grant")
		r.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		r.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an invalid grant presented alongside a session cookie got status %d, want 401.\n"+
			"The grant was refused, so the request must be refused — not answered as the cookie's user.",
			rec.Code)
	}
}

// The action registry is app-global with no surface relationship, so a grant
// minted for one surface must not reach it at all.
func TestServerActionRefusesAnEmbedGrant(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	body := `{"componentId":"other","action":"embed-probe","params":{}}`
	rec := f.do(t, http.MethodPost, "/__gofastr/action", body, func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", grant)
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("a grant for surface \"reports\" invoked surface \"other\"'s server action.\n"+
			"Actions are keyed app-globally with no surface to check against, so accepting\n"+
			"a grant here reaches every action in the app — including from a surface with\n"+
			"no subject at all.\nbody: %s", rec.Body.String())
	}
}

// The catalog and the widget gate must follow the same rule as the shared
// endpoint gate: a presented grant decides the request. Falling back to a
// cookie let an EXPIRED embed, which the runtime keeps sending on purpose so
// the server 401s, be answered as the viewer's unrelated logged-in user, with
// the per-surface scoping skipped entirely.
func TestRefusedGrantNeverFallsBackOnWidgetRoutes(t *testing.T) {
	only := func(path string) []widget.RouteMatcher {
		return []widget.RouteMatcher{func(p string) bool { return p == path }}
	}
	scratch := router.New()
	widget.Mount(scratch, &widget.Definition{Name: "r4fall-admin", Routes: only("/admin")})

	f := newEmbedFixture(t)
	sess := f.host.CreateSession()
	withDeadGrantAndCookie := func(r *http.Request) {
		r.Header.Set("X-Gofastr-Embed", "emg_expired-or-forged")
		r.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		r.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	}

	if rec := f.do(t, http.MethodGet, "/__gofastr/widgets?page=/admin", "", withDeadGrantAndCookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("the catalog answered a refused grant using the session cookie: status %d, want 401\n%s",
			rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/core-ui/widget/r4fall-admin/chrome", nil)
	withDeadGrantAndCookie(req)
	if check := widget.SessionCheck(); check != nil && check(req) {
		t.Error("the widget gate admitted a refused grant on the strength of a session cookie, " +
			"skipping the per-surface scoping entirely")
	}
}

// A duplicate must WAIT for the in-flight owner, not resolve its own copy.
//
// Resolving independently and releasing the extra registration looked
// equivalent because the key is a content address, but when the duplicate got
// there first, its register took the refcount 0→1 and its release took it 1→0,
// deleting the variant before the owner registered anything. The duplicate then
// returned a key nothing held.
func TestDuplicateThemeWaitsForTheOwner(t *testing.T) {
	var state embedThemeState

	if ok, _, _ := state.reserve("reports", "brand-a", 2); !ok {
		t.Fatal("first reserve refused")
	}

	// The owner records shortly; the duplicate must observe that key, not "".
	go func() {
		time.Sleep(30 * time.Millisecond)
		state.record("reports", "brand-a", "variant-key")
	}()

	start := time.Now()
	key, ok := state.waitFor("reports", "brand-a", 2*time.Second)
	elapsed := time.Since(start)

	if !ok || key != "variant-key" {
		t.Fatalf("duplicate saw key=%q ok=%v; want the owner's variant-key", key, ok)
	}
	// It must be WOKEN, not merely rescued by the post-timeout lookup. Without
	// this the test passes on the timeout path and cannot see a record that
	// forgets to settle its waiter.
	if elapsed > time.Second {
		t.Errorf("duplicate waited %v — it timed out and fell back to lookup rather "+
			"than being woken when the owner recorded", elapsed)
	}
}

// And when the owner fails, the duplicate must not hang or invent a key.
func TestDuplicateGivesUpWhenTheOwnerFails(t *testing.T) {
	var state embedThemeState
	state.reserve("reports", "brand-b", 2)

	go func() {
		time.Sleep(20 * time.Millisecond)
		state.release("reports", "brand-b")
	}()

	start := time.Now()
	key, _ := state.waitFor("reports", "brand-b", 2*time.Second)
	if key != "" {
		t.Errorf("duplicate returned %q after the owner released", key)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("duplicate waited %v after the owner settled; release must wake it", elapsed)
	}
}
