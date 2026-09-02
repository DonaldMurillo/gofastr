package main

// HTTP- and source-level tests for the example's boundaries. The
// chromedp suite (browser_test.go) drives the same boundaries through
// a real browser; these pin them without one.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestApp builds a fresh app + server, isolating every test's
// state (the store is process-global so buildApp can reach it).
// noRedirectClient reads 303s instead of following them.
var noRedirectClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

func newTestApp(t *testing.T) (*httptest.Server, *assistApp) {
	t.Helper()
	app := buildApp()
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv, assist
}

// supportLogin performs the demo sign-in with the boot-time key and
// returns the cookie.
func supportLogin(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	resp, err := noRedirectClient.PostForm(srv.URL+"/support/login", url.Values{"key": {assist.supportKey}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == supportCookieName {
			return c
		}
	}
	t.Fatal("login set no support cookie")
	return nil
}

// A wrong or missing key mints nothing and answers with the same
// redirect as success, so the response does not say which it was.
func TestLoginRequiresKey(t *testing.T) {
	srv, _ := newTestApp(t)
	for _, key := range []string{"", "wrong", assist.supportKey + "x"} {
		resp, err := noRedirectClient.PostForm(srv.URL+"/support/login", url.Values{"key": {key}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/support/login" {
			t.Fatalf("key %q: %d -> %q, want 303 back to the sign-in", key, resp.StatusCode, resp.Header.Get("Location"))
		}
		if firstCookie(resp, supportCookieName) != nil {
			t.Fatalf("key %q minted a support cookie", key)
		}
	}
}

// newSupportSession logs in and creates a session, returning the
// session id and the one-time join path.
func newSupportSession(t *testing.T, srv *httptest.Server, a *assistApp) (*http.Cookie, string) {
	t.Helper()
	cookie := supportLogin(t, srv)
	resp, err := noRedirectClient.Do(withCookie(t, srv, cookie, "POST", "/support/sessions", nil))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	id := strings.TrimPrefix(loc, "/support/session/")
	if id == loc || id == "" {
		t.Fatalf("create session redirected to %q", loc)
	}
	return cookie, id
}

func withCookie(t *testing.T, srv *httptest.Server, c *http.Cookie, method, path string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(c)
	return req
}

// MarkerSpoof: the WebMCP marker header must never substitute for the
// role cookie. A spoofed marker without the cookie is 403.
func TestMarkerNeverAuthorizesToolCall(t *testing.T) {
	srv, _ := newTestApp(t)
	resp, err := srv.Client().Post(srv.URL+"/support/api/instruction",
		"application/json", strings.NewReader(`{"session":"x","instruction":"spoof"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/support/api/instruction",
		strings.NewReader(`{"session":"x","instruction":"spoof"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gofastr-WebMCP", "1")
	resp2, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp.StatusCode != http.StatusForbidden || resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("marker-spoofed call without cookie = %d/%d, want 403/403", resp.StatusCode, resp2.StatusCode)
	}
}

// A real cookie without the marker works (manual path) and produces NO
// invocation event: the marker attributes, it does not grant — and its
// absence is not a refusal either.
func TestUnmarkedCallWorksButIsNotInvocation(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	resp, err := srv.Client().Do(withCookie(t, srv, cookie, "POST", "/support/api/instruction",
		strings.NewReader(`{"session":"`+id+`","instruction":"turn it off and on"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manual tool call = %d, want 200", resp.StatusCode)
	}
	for _, line := range a.toolEventLog() {
		if strings.Contains(line, "invoke") {
			t.Fatalf("unmarked call produced an invocation event: %q", line)
		}
	}
}

// The marked call is attributed: an invoke event with a correlation id.
func TestMarkedCallRecordsInvocation(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	req, _ := http.NewRequest("POST", srv.URL+"/support/api/instruction",
		strings.NewReader(`{"session":"`+id+`","instruction":"press restart"}`))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gofastr-WebMCP", "1")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("marked call = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Gofastr-WebMCP-Invocation") == "" {
		t.Fatal("marked call carries no invocation header")
	}
	found := false
	for _, line := range a.toolEventLog() {
		if strings.HasPrefix(line, "invoke send_instruction") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no invoke event for send_instruction in %v", a.toolEventLog())
	}
}

// One typed command: the console's form-encoded POST and the tool's
// JSON POST mutate the same state through the same applyCommand.
func TestManualFormAndToolShareOneCommand(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)

	form := withCookie(t, srv, cookie, "POST", "/support/session/"+id+"/instruction",
		strings.NewReader("instruction=press+the+green+button"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := noRedirectClient.Do(form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("manual form = %d, want 303", resp.StatusCode)
	}

	snap := inspect(t, srv, cookie, id)
	if snap.Instruction != "press the green button" {
		t.Fatalf("after manual form, instruction = %q", snap.Instruction)
	}

	tool := withCookie(t, srv, cookie, "POST", "/support/api/clear",
		strings.NewReader(`{"session":"`+id+`"}`))
	tool.Header.Set("Content-Type", "application/json")
	resp2, err := srv.Client().Do(tool)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	snap = inspect(t, srv, cookie, id)
	if snap.Instruction != "" {
		t.Fatalf("after tool clear, instruction = %q, want empty", snap.Instruction)
	}
}

// The join link is single-use: first open trades it for the operator
// cookie, second open renders the 410 recovery page.
func TestJoinLinkIsSingleUse(t *testing.T) {
	srv, a := newTestApp(t)
	_, id := newSupportSession(t, srv, a)
	join := "/join/" + a.lookup(id).joinToken

	resp, err := noRedirectClient.Get(srv.URL + join)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("first join = %d, want 303 (body %q)", resp.StatusCode, body)
	}

	resp2, err := noRedirectClient.Get(srv.URL + join)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	page, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusGone {
		t.Fatalf("second join = %d, want 410", resp2.StatusCode)
	}
	if !strings.Contains(string(page), "assist session has ended") {
		t.Fatalf("second join body is not the recovery screen")
	}
}

// An operator cookie minted for session A authorizes nothing on
// session B, and the operator page guard answers 410 with the same
// recovery screen for a missing cookie and an unknown session.
func TestOperatorCookieIsSessionScoped(t *testing.T) {
	srv, a := newTestApp(t)
	_, idA := newSupportSession(t, srv, a)
	_, idB := newSupportSession(t, srv, a)

	resp, err := noRedirectClient.Get(srv.URL + "/join/" + a.lookup(idB).joinToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	opCookie := firstCookie(resp, operatorCookieName)
	if opCookie == nil {
		t.Fatal("join set no operator cookie")
	}

	// Right session: 200.
	req, _ := http.NewRequest("GET", srv.URL+"/session/"+idB, nil)
	req.AddCookie(opCookie)
	resp2, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("operator page with own cookie = %d, want 200", resp2.StatusCode)
	}

	// Wrong session and no cookie: both 410, same body.
	req2, _ := http.NewRequest("GET", srv.URL+"/session/"+idA, nil)
	req2.AddCookie(opCookie)
	resp3, err := noRedirectClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	page, _ := io.ReadAll(resp3.Body)
	if resp3.StatusCode != http.StatusGone || !strings.Contains(string(page), "assist session has ended") {
		t.Fatalf("foreign session = %d, want 410 recovery", resp3.StatusCode)
	}
	resp4, err := noRedirectClient.Get(srv.URL + "/session/" + idB)
	if err != nil {
		t.Fatal(err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusGone {
		t.Fatalf("operator page without cookie = %d, want 410", resp4.StatusCode)
	}
}

// The operator's write route is guarded by the group middleware: no
// cookie and a cookie for another session are both 403 before the
// handler runs, and the right cookie acknowledges.
func TestAckRequiresMatchingOperatorCookie(t *testing.T) {
	srv, a := newTestApp(t)
	_, idA := newSupportSession(t, srv, a)
	_, idB := newSupportSession(t, srv, a)
	resp, err := noRedirectClient.Get(srv.URL + "/join/" + a.lookup(idB).joinToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	opCookie := firstCookie(resp, operatorCookieName)
	post := func(id string, c *http.Cookie) int {
		req, _ := http.NewRequest("POST", srv.URL+"/session/"+id+"/ack", nil)
		if c != nil {
			req.AddCookie(c)
		}
		r, err := noRedirectClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if got := post(idB, nil); got != http.StatusForbidden {
		t.Fatalf("ack without cookie = %d, want 403", got)
	}
	if got := post(idA, opCookie); got != http.StatusForbidden {
		t.Fatalf("ack on another session = %d, want 403", got)
	}
	if got := post(idB, opCookie); got != http.StatusSeeOther {
		t.Fatalf("ack with own cookie = %d, want 303", got)
	}
	if !a.lookup(idB).acked {
		t.Fatal("ack did not record")
	}
}

// The bridge document scope: support pages carry the bridge tag, the
// landing and operator pages carry nothing.
func TestBridgeShipsOnlyOnSupportPages(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	join := "/join/" + a.lookup(id).joinToken

	resp, err := noRedirectClient.Get(srv.URL + join)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	opCookie := firstCookie(resp, operatorCookieName)

	for _, path := range []string{"/", "/support/login"} {
		page := getPage(t, srv, path)
		if strings.Contains(page, `src="/__gofastr/webmcp.js`) || strings.Contains(page, "data-fui-doc") {
			t.Fatalf("%s carries a document-scoped script", path)
		}
	}

	req, _ := http.NewRequest("GET", srv.URL+"/session/"+id, nil)
	req.AddCookie(opCookie)
	resp2, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	opBody, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	// app.js IS document-scoped here (the operator page needs it);
	// the WebMCP bridge must not be.
	if strings.Contains(string(opBody), `src="/__gofastr/webmcp.js`) {
		t.Fatal("operator page carries the WebMCP bridge")
	}

	console := getPage(t, srv, "/support/session/"+id, cookie)
	if !strings.Contains(console, "data-fui-doc") {
		t.Fatal("support console lacks the document-scoped bridge tag")
	}
	if !strings.Contains(console, "/__assist/app.js") {
		t.Fatal("support console lacks app.js")
	}
}

// The WebMCP asset routes require the support cookie: the manifest
// names every tool, so discovery is gated like the endpoints.
func TestManifestRequiresSupportRole(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, _ := newSupportSession(t, srv, a)

	resp, err := srv.Client().Get(srv.URL + "/__gofastr/webmcp/tools.json")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous manifest = %d, want 403", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/__gofastr/webmcp/tools.json", nil)
	req.AddCookie(cookie)
	resp2, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	for _, name := range []string{"inspect_session", "send_instruction", "clear_instruction", "get_app_instructions"} {
		if !strings.Contains(string(body), name) {
			t.Fatalf("manifest lacks %q", name)
		}
	}
}

// A cross-site mutating request is refused even with a valid cookie
// (the CSRF posture: the role cookie decides WHO, Fetch Metadata and
// Origin decide WHERE FROM). Sec-Fetch-Site is authoritative: a
// cross-site request is refused whatever Origin says, and a same-origin
// one passes. Without Fetch Metadata the Origin host decides, and a
// null Origin (a top-level same-origin form navigation) passes.
func TestCrossSiteCommandRefused(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	cases := []struct {
		name   string
		sfs    string
		origin string
		want   int
	}{
		{"cross-site metadata", "cross-site", "", http.StatusForbidden},
		{"cross-site metadata beats a matching origin", "cross-site", srv.URL, http.StatusForbidden},
		{"same-origin metadata", "same-origin", "", http.StatusOK},
		{"foreign origin, no metadata", "", "https://evil.example", http.StatusForbidden},
		{"matching origin, no metadata", "", srv.URL, http.StatusOK},
		{"null origin, no metadata", "", "null", http.StatusOK},
		{"no headers at all", "", "", http.StatusOK},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest("POST", srv.URL+"/support/api/instruction",
			strings.NewReader(`{"session":"`+id+`","instruction":"x"}`))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		if tc.sfs != "" {
			req.Header.Set("Sec-Fetch-Site", tc.sfs)
		}
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
}

// Every published event advances the session version, signaling
// frames included: the channel assigns each event the next sequence,
// and a snapshot whose sequence trailed the events a page had applied
// would be refused by the page's reducer, leaving a reconnect
// unhydrated.
func TestSignalAdvancesSessionVersion(t *testing.T) {
	_, a := newTestApp(t)
	s := a.createSession()
	src := sessionSource{app: a, id: s.id}
	_, before := src.SnapshotFor(roleSupport)
	if !a.relaySignal(s, roleOperator, signalMessage{To: roleSupport, Type: "offer", Data: json.RawMessage(`{}`)}) {
		t.Fatal("offer to the peer was refused")
	}
	if a.relaySignal(s, roleOperator, signalMessage{To: roleOperator, Type: "offer", Data: json.RawMessage(`{}`)}) {
		t.Fatal("offer addressed to the sender's own role was relayed")
	}
	_, after := src.SnapshotFor(roleSupport)
	if after != before+1 {
		t.Fatalf("session version %d -> %d after one relayed frame, want +1", before, after)
	}
}

// The microphone is denied by the response header, not only by the
// getUserMedia constraints: a page script that asked for audio would
// be refused by the browser. The camera is open to this origin only.
func TestMicrophoneDeniedByPolicy(t *testing.T) {
	srv, _ := newTestApp(t)
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	pp := resp.Header.Get("Permissions-Policy")
	if !strings.Contains(pp, "microphone=()") || !strings.Contains(pp, "camera=(self)") {
		t.Fatalf("Permissions-Policy = %q, want microphone denied and camera self", pp)
	}
}

// A bad command reports why, not a generic status text.
func TestBadRequestKeepsItsReason(t *testing.T) {
	_, a := newTestApp(t)
	s := a.createSession()
	_, status, err := a.applyCommand(assistCommand{Session: s.id, Kind: cmdInstruction, Instruction: "   "})
	if status != http.StatusBadRequest || err == nil || !strings.Contains(err.Error(), "1-500") {
		t.Fatalf("blank instruction: %d %v", status, err)
	}
}

// The session pages are per-role state (instruction text, the join
// link) and must not enter shared or history caches. The host sends
// Cache-Control: no-store on every rendered page; this pins the
// contract the example relies on rather than adding a header of its
// own.
func TestSessionPagesAreNoStore(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	req, _ := http.NewRequest("GET", srv.URL+"/support/session/"+id, nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); resp.StatusCode != http.StatusOK || !strings.Contains(cc, "no-store") {
		t.Fatalf("console = %d Cache-Control %q", resp.StatusCode, cc)
	}
	resp, err = noRedirectClient.Get(srv.URL + "/join/" + a.lookup(id).joinToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req2, _ := http.NewRequest("GET", srv.URL+"/session/"+id, nil)
	req2.AddCookie(firstCookie(resp, operatorCookieName))
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if cc := resp2.Header.Get("Cache-Control"); resp2.StatusCode != http.StatusOK || !strings.Contains(cc, "no-store") {
		t.Fatalf("operator page = %d Cache-Control %q", resp2.StatusCode, cc)
	}
}

// Source-level minimization: signaling is addressed, the invocation
// ref is support-only, and a hidden event serializes nothing.
func TestFilterEventMinimizesPerRole(t *testing.T) {
	_, a := newTestApp(t)
	s := a.createSession()
	src := sessionSource{app: a, id: s.id}

	offer := assistEvent{Kind: "signal", Signal: &signalMessage{From: roleOperator, To: roleSupport, Type: "offer", Data: json.RawMessage(`{"sdp":"v=0"}`)}}
	if _, ok := src.FilterEvent(roleOperator, offer); ok {
		t.Fatal("operator received its own offer back")
	}
	seen, ok := src.FilterEvent(roleSupport, offer)
	if !ok {
		t.Fatal("support did not receive the offer")
	}
	if _, ok := seen.(signalMessage); !ok {
		t.Fatalf("offer payload type %T", seen)
	}

	a.applyCommand(assistCommand{Session: s.id, Kind: cmdInstruction, Instruction: "look left", Ref: "inv-123"})
	if _, snapSeq := src.SnapshotFor(roleOperator); snapSeq == 0 {
		t.Fatal("snapshot sequence is zero after a mutation")
	}
	opSnap, _ := src.SnapshotFor(roleOperator)
	supSnap, _ := src.SnapshotFor(roleSupport)
	if opSnap.Invocation != "" {
		t.Fatalf("operator snapshot carries the invocation ref %q", opSnap.Invocation)
	}
	if supSnap.Invocation != "inv-123" {
		t.Fatalf("support snapshot invocation = %q", supSnap.Invocation)
	}

	for _, ev := range []assistEvent{
		{Kind: cmdAck, Acked: true, Ref: "inv-123"},
		{Kind: cmdInstruction, Instruction: "look left", Ref: "inv-123"},
		{Kind: cmdClear, Ref: "inv-123"},
	} {
		op, ok := src.FilterEvent(roleOperator, ev)
		if !ok {
			t.Fatalf("operator did not receive the %s event", ev.Kind)
		}
		if strings.Contains(mustJSON(t, op), "inv-123") {
			t.Fatalf("operator %s payload carries the invocation ref: %s", ev.Kind, mustJSON(t, op))
		}
		sup, _ := src.FilterEvent(roleSupport, ev)
		if !strings.Contains(mustJSON(t, sup), "inv-123") {
			t.Fatalf("support %s payload lost the invocation ref: %s", ev.Kind, mustJSON(t, sup))
		}
	}
}

// A dead session answers 410 everywhere it is addressable.
func TestDeadSessionIsGone(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	s := a.lookup(id)
	a.mu.Lock()
	a.dropLocked(s)
	a.mu.Unlock()

	req, _ := http.NewRequest("GET", srv.URL+"/support/session/"+id, nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusGone || !strings.Contains(string(page), "assist session has ended") {
		t.Fatalf("dead session console = %d, want 410 recovery", resp.StatusCode)
	}
	if code := inspectStatus(t, srv, cookie, id); code != http.StatusGone {
		t.Fatalf("inspect on dead session = %d, want 410", code)
	}
}

// ── helpers ──────────────────────────────────────────────────────

func getPage(t *testing.T, srv *httptest.Server, path string, cookies ...*http.Cookie) string {
	t.Helper()
	req, err := http.NewRequest("GET", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func firstCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func inspect(t *testing.T, srv *httptest.Server, cookie *http.Cookie, id string) assistSnapshot {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/support/api/inspect?session="+id, nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var snap assistSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("inspect body: %v", err)
	}
	return snap
}

func inspectStatus(t *testing.T, srv *httptest.Server, cookie *http.Cookie, id string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/support/api/inspect?session="+id, nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
