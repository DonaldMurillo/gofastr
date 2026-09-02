package main

// Browser end-to-end: one Chrome, two tabs, fake media, the real
// WebMCP browser API. Covers the flow the docs describe end to end:
// discovery on the support document only, one command path behind
// both the manual button and the agent tool, the operator
// acknowledgement, transport death without state resurrection, and
// the hard navigation out of the capability-bearing page.
//
// Skips in -short mode. One browser for the whole suite: the tabs
// share a cookie jar, and the roles stay separated by which page tree
// each cookie is scoped to (see session.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// assistBrowserCtx boots a browser for the test with the flags the flow
// needs (WebMCP exposed, fake camera device, fake grant UI) and tears
// it down with the test: both the allocator and the browser context
// are cancelled from t.Cleanup, so no Chrome outlives the suite.
func assistBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// Exposes navigator.modelContext (Chromium 146+).
		chromedp.Flag("enable-blink-features", "WebMCP"),
		// getUserMedia without a real camera or permission dialog.
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	root, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	if err := chromedp.Run(root); err != nil {
		t.Fatalf("browser failed to start: %v", err)
	}
	return root
}

func newTab(t *testing.T, browser context.Context) context.Context {
	t.Helper()
	tab, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	ctx, tcancel := context.WithTimeout(tab, 150*time.Second)
	t.Cleanup(tcancel)
	return ctx
}

// evalAwait evaluates an expression, awaiting any promise it returns.
func evalAwait(ctx context.Context, expr string, out *string) error {
	return chromedp.Run(ctx, chromedp.Evaluate(expr, out,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
}

// execTool drives the real WebMCP browser API: find the tool handle
// by name and hand it to executeTool with the input as a JSON string,
// which resolves with the execute() result JSON-stringified (Chromium
// 151 behavior, the shape the webmcp package's own browser test uses).
func execTool(ctx context.Context, name, inputJSON string) (string, error) {
	expr := fmt.Sprintf(`(async () => {
		const mc = document.modelContext || navigator.modelContext;
		const tools = await mc.getTools();
		const tool = tools.find(t => t.name === %q);
		if (!tool) return JSON.stringify({error: "not registered"});
		return mc.executeTool(tool, JSON.stringify(%s));
	})()`, name, inputJSON)
	var raw string
	if err := evalAwait(ctx, expr, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

// toolText unwraps the bridge's result envelope ({content:[{text}]},
// JSON-stringified by executeTool) to the endpoint's response body.
func toolText(t *testing.T, raw string) string {
	t.Helper()
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil || len(res.Content) == 0 {
		t.Fatalf("tool result envelope %q: %v", raw, err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error result: %s", res.Content[0].Text)
	}
	return res.Content[0].Text
}

const toolNamesExpr = `(async () =>
	(await (document.modelContext || navigator.modelContext).getTools())
		.map(t => t.name).sort().join(','))()`

// pollExpr polls an (optionally async) string expression until it
// equals want.
func pollExpr(t *testing.T, ctx context.Context, expr, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got string
	for {
		if err := evalAwait(ctx, expr, &got); err != nil {
			t.Fatalf("poll: %v", err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poll = %q, want %q (expr %s, deadline)", got, want, expr)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func waitReady(t *testing.T, ctx context.Context, sel string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery)); err != nil {
		t.Fatalf("wait %s: %v", sel, err)
	}
}

func clickSel(t *testing.T, ctx context.Context, sel string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
		t.Fatalf("click %s: %v", sel, err)
	}
}

func evalString(t *testing.T, ctx context.Context, expr string) string {
	t.Helper()
	var got string
	if err := evalAwait(ctx, expr, &got); err != nil {
		t.Fatalf("eval %s: %v", expr, err)
	}
	return got
}

// TestRemoteAssistFlow drives support and operator through one
// session: fake camera over WebRTC, manual + agent commands sharing
// one command path, the ack, transport death and recovery without
// state resurrection, discovery boundaries, and the hard navigation
// out of the console.
func TestRemoteAssistFlow(t *testing.T) {
	app := buildApp()
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	browser := assistBrowserCtx(t)
	support := newTab(t, browser)  // tab 1: support console
	operator := newTab(t, browser) // tab 2: operator page

	// ── 1. Landing: zero tools in the document ───────────────────
	if err := chromedp.Run(support, chromedp.Navigate(srv.URL+"/")); err != nil {
		t.Fatal(err)
	}
	pollExpr(t, support, toolNamesExpr, "", 15*time.Second)

	// ── 2. Support signs in, creates a session ───────────────────
	if err := chromedp.Run(support, chromedp.Navigate(srv.URL+"/support/login")); err != nil {
		t.Fatal(err)
	}
	waitReady(t, support, `form[action="/support/login"] button[type=submit]`)
	if err := chromedp.Run(support, chromedp.SetValue("#assist-support-key", assist.supportKey, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	clickSel(t, support, `form[action="/support/login"] button[type=submit]`)
	waitReady(t, support, `form[action="/support/sessions"] button[type=submit]`)
	clickSel(t, support, `form[action="/support/sessions"] button[type=submit]`)
	waitReady(t, support, "#assist-root")

	sid := evalString(t, support, `document.getElementById('assist-root').dataset.assistSession`)
	joinURL := evalString(t, support, `document.getElementById('assist-join-link').value`)
	if sid == "" || joinURL == "" {
		t.Fatalf("session %q join %q", sid, joinURL)
	}

	// ── 3. Discovery: four tools on the console document ─────────
	pollExpr(t, support, toolNamesExpr,
		"clear_instruction,get_app_instructions,inspect_session,send_instruction", 15*time.Second)
	pollExpr(t, support, `window.__assist.phase`, "hydrated", 15*time.Second)

	// ── 4. Operator joins: the link opens a confirmation page (a
	// previewer fetching it spends nothing); the button's POST is
	// the one-time exchange. Zero tools on the operator document.
	if err := chromedp.Run(operator, chromedp.Navigate(joinURL)); err != nil {
		t.Fatal(err)
	}
	waitReady(t, operator, `form[action^="/join/"] button[type=submit]`)
	clickSel(t, operator, `form[action^="/join/"] button[type=submit]`)
	waitReady(t, operator, "#assist-share")
	pollExpr(t, operator, toolNamesExpr, "", 15*time.Second)
	pollExpr(t, operator, `window.__assist.phase`, "hydrated", 15*time.Second)

	// Support sees the operator arrive.
	pollExpr(t, support, `'' + !document.getElementById('assist-pill-op-on').hidden`, "true", 15*time.Second)

	// ── 5. Camera: peer-to-peer, server sees only signaling ──────
	clickSel(t, operator, "#assist-share")
	pollExpr(t, operator, `window.__assist.pcState`, "connected", 30*time.Second)
	pollExpr(t, support, `window.__assist.pcState`, "connected", 30*time.Second)
	pollExpr(t, support, `'' + !!document.getElementById('assist-remote').srcObject`, "true", 15*time.Second)
	pollExpr(t, support, `'' + !document.getElementById('assist-pill-media-on').hidden`, "true", 15*time.Second)

	// The microphone never exists: the operator's stream is video-only.
	pollExpr(t, operator,
		`'' + (() => {
			const v = document.getElementById('assist-local');
			return !!v && !!v.srcObject
				&& v.srcObject.getAudioTracks().length === 0
				&& v.srcObject.getVideoTracks().length > 0;
		})()`, "true", 15*time.Second)

	// ── 6. Manual button and agent tool share one command path ────
	if err := chromedp.Run(support, chromedp.SetValue("#assist-instruction-input",
		"Turn the dial to three.", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	clickSel(t, support, `#assist-manual-form button[type=submit]`)
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"Turn the dial to three.", 15*time.Second)

	out, err := execTool(support, "send_instruction",
		fmt.Sprintf(`{"session":%q,"instruction":"Now press the blue button."}`, sid))
	if err != nil {
		t.Fatalf("send_instruction: %v", err)
	}
	if !strings.Contains(out, "accepted") {
		t.Fatalf("send_instruction result: %s", out)
	}
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"Now press the blue button.", 15*time.Second)
	// The invocation ref is support-only correlation data.
	pollExpr(t, support, `'' + document.getElementById('assist-invocation').textContent.includes('invocation')`,
		"true", 15*time.Second)

	// ── 7. Operator acknowledges the rendered instruction ─────────
	clickSel(t, operator, `#assist-ack-form button[type=submit]`)
	pollExpr(t, operator, `'' + !document.getElementById('assist-pill-ack-on').hidden`, "true", 15*time.Second)
	pollExpr(t, support, `'' + !document.getElementById('assist-pill-ack-on').hidden`, "true", 15*time.Second)

	// Verify delivery the honest way: the tool reads backend state.
	inspect, err := execTool(support, "inspect_session", fmt.Sprintf(`{"session":%q}`, sid))
	if err != nil {
		t.Fatalf("inspect_session: %v", err)
	}
	if text := toolText(t, inspect); !strings.Contains(text, `"acked":true`) {
		t.Fatalf("inspect_session after ack: %s", text)
	}

	// ── 8. Transport death cannot resurrect cleared state ────────
	if _, err := execTool(support, "send_instruction",
		fmt.Sprintf(`{"session":%q,"instruction":"Unplug the cable for ten seconds."}`, sid)); err != nil {
		t.Fatal(err)
	}
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"Unplug the cable for ten seconds.", 15*time.Second)
	if _, err := execTool(support, "clear_instruction", fmt.Sprintf(`{"session":%q}`, sid)); err != nil {
		t.Fatal(err)
	}
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"No instruction yet.", 15*time.Second)

	// Instruction envelopes applied so far; a reconnect that replayed
	// stale state would grow this count.
	appliedBefore := evalString(t, operator,
		`'' + window.__assist.applied.filter(e => e.startsWith('instruction')).length`)

	// Server drops the operator's socket: the page's ws module must
	// classify, reconnect, and rehydrate — without resurrecting the
	// cleared instruction, and without tearing down the healthy peer.
	// Nothing changed while the socket was down, so the reconnect
	// snapshot is not newer than the page's state; hydration must
	// complete anyway.
	if !assist.dropRoleSocket(sid, roleOperator) {
		t.Fatal("no operator socket to drop")
	}
	pollExpr(t, operator, `'' + (window.__assist.generations >= 2)`, "true", 30*time.Second)
	pollExpr(t, operator, `window.__assist.phase`, "hydrated", 30*time.Second)
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"No instruction yet.", 15*time.Second)
	appliedAfter := evalString(t, operator,
		`'' + window.__assist.applied.filter(e => e.startsWith('instruction')).length`)
	if appliedAfter != appliedBefore {
		t.Fatalf("reconnect resurrected instruction state: %s -> %s", appliedBefore, appliedAfter)
	}
	// The media path is peer-to-peer: it survives the transport drop.
	pollExpr(t, operator, `window.__assist.pcState`, "connected", 15*time.Second)

	// Second drop, and a mutation lands while the operator is offline
	// (the reconnect backoff is one second; the tool call takes
	// milliseconds). The event is gone for good; only the reconnect
	// snapshot can carry the new text. A snapshot whose sequence
	// trailed the events the page had applied (the signaling frames
	// relayed earlier consumed sequences too) would be refused as
	// stale, and the page would keep showing the cleared slot.
	if !assist.dropRoleSocket(sid, roleOperator) {
		t.Fatal("no operator socket to drop (second)")
	}
	if _, err := execTool(support, "send_instruction",
		fmt.Sprintf(`{"session":%q,"instruction":"Sent while you were offline."}`, sid)); err != nil {
		t.Fatal(err)
	}
	pollExpr(t, operator, `'' + (window.__assist.generations >= 3)`, "true", 30*time.Second)
	pollExpr(t, operator, `window.__assist.phase`, "hydrated", 30*time.Second)
	pollExpr(t, operator, `document.getElementById('assist-instruction-text').textContent`,
		"Sent while you were offline.", 15*time.Second)
	appliedOffline := evalString(t, operator,
		`'' + window.__assist.applied.filter(e => e.startsWith('instruction')).length`)
	if appliedOffline != appliedBefore {
		t.Fatalf("the missed instruction event was replayed: %s -> %s", appliedBefore, appliedOffline)
	}

	// ── 9. Leaving support is a hard navigation; tools retire ────
	if err := chromedp.Run(support, chromedp.Evaluate(`window.__marker = 'support-doc'`, nil)); err != nil {
		t.Fatal(err)
	}
	clickSel(t, support, `nav a[href="/"]`)
	pollExpr(t, support, `'' + (typeof window.__marker)`, "undefined", 30*time.Second)
	pollExpr(t, support, toolNamesExpr, "", 15*time.Second)
}
