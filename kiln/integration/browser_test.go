package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	_ "github.com/mattn/go-sqlite3"

	"github.com/gofastr/gofastr/kiln/chat"
	kilnmcp "github.com/gofastr/gofastr/kiln/agent/mcp"
	"github.com/gofastr/gofastr/kiln/db"
	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
	"github.com/gofastr/gofastr/kiln/world"
	"github.com/gofastr/gofastr/framework"
)

// testInFlight is the integration-test handle for the simulated
// in_flight bit consumed by the AgentStateFn passed to MountPanel.
// Tests flip it via Store/Reset to drive the in-flight indicator
// without needing a real agent subprocess.
var testInFlight atomic.Bool

// testCurrentAgent is the simulated current-adapter name consumed
// by the test AgentStateFn so tests that assert the header chip
// updates can flip it and notify "agent_changed".
var testCurrentAgent atomic.Value // string

// stand a live kiln server up on httptest. Same wiring as cmd/kiln.
func startKiln(t *testing.T) (string, *protocol.Tools) {
	srvURL, _, tools := startKilnExt(t)
	return srvURL, tools
}

// startKilnExt is startKiln + the *live.Live pointer so tests can drive
// synthetic SSE events (l.Notify) and a flippable in_flight callback so
// tests can simulate an agent turn.
func startKilnExt(t *testing.T) (string, *live.Live, *protocol.Tools) {
	t.Helper()
	testInFlight.Store(false)
	testCurrentAgent.Store("none")
	t.Cleanup(func() {
		testInFlight.Store(false)
		testCurrentAgent.Store("none")
	})
	d, cleanup, err := db.EphemeralSQLite("kiln-browser")
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
	chatSrv := chat.New(l, tools)
	chatSrv.Mount(l.Aux())
	chat.MountPanel(l.Aux(), l, tools, func() any {
		// Stub agent state for the integration tests — at least one
		// installed adapter so the modal list isn't empty. Reads
		// in_flight + current from package-level test atomics so
		// tests can simulate turns and adapter switches.
		curName, _ := testCurrentAgent.Load().(string)
		if curName == "" {
			curName = "none"
		}
		return map[string]any{
			"current": map[string]any{"name": curName, "display": "(stub: " + curName + ")"},
			"available": []map[string]any{
				{"name": "claude-code", "display": "Claude Code CLI", "installed": true},
				{"name": "pi", "display": "pi", "installed": false},
				{"name": "codex", "display": "OpenAI Codex CLI", "installed": false},
			},
			"in_flight": testInFlight.Load(),
		}
	})
	l.SetFallbackFunc(chat.HostHTMLForLive(l))

	// Stub /kiln/agent so the modal Apply form has somewhere to POST.
	// Real wiring lives in cmd/kiln/agent_http.go; tests only need a
	// 200 ack so the runtime's data-fui-rpc-close path triggers.
	l.Aux().Post("/kiln/agent", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"current":{"name":"claude-code"}}`))
	}))

	mcpSrv, err := kilnmcp.NewServer(tools)
	if err != nil {
		t.Fatal(err)
	}
	l.Aux().Handle("POST", "/mcp", mcpSrv)

	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	return srv.URL, l, tools
}

func newChrome(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	alloc, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browser, browserCancel := chromedp.NewContext(alloc)
	t.Cleanup(browserCancel)
	ctx, timeoutCancel := context.WithTimeout(browser, 30*time.Second)
	return ctx, timeoutCancel
}

// --- (1) widget loads on host fallback -------------------------------

func TestBrowser_HostShowsWidget(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-panel.kiln-open`, chromedp.ByQuery),
		chromedp.OuterHTML(`.kiln-widget`, &html, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	for _, want := range []string{"kiln-panel-head", "kiln-input", "Send"} {
		if !strings.Contains(html, want) {
			t.Errorf("widget missing %q in DOM: %s", want, html[:min(len(html), 400)])
		}
	}
}

// --- (2) typing in the widget journals a message --------------------

func TestBrowser_SendMessageFromWidget(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		chromedp.SendKeys(`.kiln-input`, "hello kiln"),
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
		// Server roundtrip + SSE refresh.
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("interact: %v", err)
	}

	// Verify the journal recorded it (page-prefixed by the widget).
	chat := tools.Live().Session().Chat
	if len(chat) == 0 || chat[0].Message == nil {
		t.Fatalf("no chat journaled: %+v", chat)
	}
	if !strings.Contains(chat[0].Message.Text, "hello kiln") {
		t.Errorf("text mismatch: %q", chat[0].Message.Text)
	}
}

// --- (3) external add_entity propagates to widget via SSE -----------

func TestBrowser_ExternalAddEntityShowsInWidget(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Wait for SSE to connect. EventSource sets __fuiSSEReady on open.
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			var diag string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`(function(){
				return JSON.stringify({
					mounted: !!window.__kilnWidgetMounted,
					ready: !!window.__fuiSSEReady,
					hasFAB: !!document.querySelector(".kiln-fab"),
					hasInput: !!document.querySelector(".kiln-input"),
				});
			})()`, &diag))
			t.Fatalf("SSE never opened: diag=%s", diag)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Trigger an add_entity from outside the browser.
	res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	}})
	if !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}

	// SSE should have caused the widget to either reload (we're on host
	// so no reload) and append a system row. Wait for the row to appear.
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`.kiln-msg-tool`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("waiting for system row: %v", err)
	}
	var rowText string
	if err := chromedp.Run(ctx,
		chromedp.Text(`.kiln-msg-tool`, &rowText, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !strings.Contains(rowText, "add_entity") {
		t.Errorf("system row missing op: %q", rowText)
	}
}

// --- (4) agent-built page is reachable + carries the widget --------

func TestBrowser_AgentAddedPageRenders(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path:  "/dashboard",
		Title: "Dashboard",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Dashboard"}},
			{Kind: "paragraph", Children: []world.Node{
				{Kind: "text", Props: map[string]any{"value": "agent built me"}},
			}},
		}},
	}})
	if !res.OK {
		t.Fatalf("add_page: %+v", res)
	}

	var heading, body string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/dashboard"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.Text(`h1`, &heading, chromedp.ByQuery),
		chromedp.Text(`body`, &body, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate dashboard: %v", err)
	}
	if !strings.Contains(heading, "Dashboard") {
		t.Errorf("h1 = %q", heading)
	}
	if !strings.Contains(body, "agent built me") {
		t.Errorf("body missing paragraph: %q", body)
	}
}

// --- (5) data-kiln-tool button fires the tool ----------------------

func TestBrowser_ButtonToolCallFires(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Build a page with a button that, when clicked, fires `chat`.
	args := map[string]any{"role": "user", "text": "fired from button"}
	argsJSON, _ := json.Marshal(args)

	res := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/build",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Build"}},
			{Kind: "button", Props: map[string]any{
				"id":             "fire",
				"label":          "Fire it",
				"data-kiln-tool": "chat",
				"data-kiln-args": string(argsJSON),
			}},
		}},
	}})
	if !res.OK {
		t.Fatalf("add_page: %+v", res)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/build"),
		chromedp.WaitVisible(`#fire`, chromedp.ByQuery),
		chromedp.Click(`#fire`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click: %v", err)
	}

	// Click triggers a fetch + soft reload (because not on host).
	// Wait briefly for the chat to land in the journal.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c := tools.Live().Session().Chat
		if len(c) > 0 && c[len(c)-1].Message != nil &&
			strings.Contains(c[len(c)-1].Message.Text, "fired from button") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("chat never journaled the button-fired message; chat=%+v", tools.Live().Session().Chat)
}

// --- (6) form posts to entity CRUD endpoint ------------------------

func TestBrowser_FormSubmitCreatesRow(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "notes",
		Fields: []world.Field{
			{Name: "text", Type: "string", Required: true},
		},
	}}); !r.OK {
		t.Fatal(r)
	}

	if r := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path: "/new-note",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "New Note"}},
			{Kind: "form", Props: map[string]any{"id": "f", "method": "POST", "action": "/notes"}, Children: []world.Node{
				{Kind: "input", Props: map[string]any{"id": "txt", "name": "text", "type": "text"}},
				{Kind: "button", Props: map[string]any{"id": "submit", "type": "submit", "label": "Save"}},
			}},
		}},
	}}); !r.OK {
		t.Fatal(r)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/new-note"),
		chromedp.WaitVisible(`#txt`, chromedp.ByQuery),
		chromedp.SendKeys(`#txt`, "a brand new note"),
		chromedp.Click(`#submit`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// The widget's submit handler posts JSON to /notes then reloads.
	// Verify the row landed by polling the CRUD listing directly with
	// the test's HTTP client — no need to fight the reload race.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGet(t, urlBase+"/notes")
		if err == nil && strings.Contains(resp, "a brand new note") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("note never landed in /notes")
}

// httpGet is a tiny helper for tests that want to bypass chromedp.
// Reads the full response — earlier 4KB cap silently truncated bodies
// large enough to hide the "paths" section of OpenAPI specs.
func httpGet(t *testing.T, url string) (string, error) {
	t.Helper()
	resp, err := newHTTPClient().Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

// --- (7) OpenAPI gets mounted once entities exist ------------------

func TestBrowser_OpenAPIServed(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string"}},
	}}); !r.OK {
		t.Fatal(r)
	}

	var spec string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/openapi.json"),
		chromedp.Text(`body`, &spec, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate openapi: %v", err)
	}
	if !strings.Contains(spec, "openapi") {
		t.Errorf("openapi.json missing 'openapi' field: %s", spec[:min(len(spec), 400)])
	}
	if !strings.Contains(spec, "/posts") {
		t.Errorf("spec missing /posts: %s", spec[:min(len(spec), 400)])
	}
}

// --- (8) seed rows actually land in the DB after add_seed ---------

func TestBrowser_SeedRowsVisibleAfterAddSeed(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name:   "tasks",
		Fields: []world.Field{{Name: "label", Type: "string", Required: true}},
	}}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "tasks",
		Rows:   []map[string]any{{"label": "buy milk"}, {"label": "write more tests"}},
	}}); !r.OK {
		t.Fatal(r)
	}

	var listing string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/tasks"),
		chromedp.Text(`body`, &listing, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate /tasks: %v", err)
	}
	for _, want := range []string{"buy milk", "write more tests"} {
		if !strings.Contains(listing, want) {
			t.Errorf("seed row %q missing: %s", want, listing)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- (8) build banner + tool-row summary feedback ------------------
//
// Verifies the user-facing feedback signals introduced for build mode:
//   * a top-of-page banner flashes on every world_edit even when the
//     panel is collapsed
//   * the panel tool-call rows render a glanceable summary
//     (name=foo fields=N) instead of raw JSON.
func TestBrowser_BuildBannerFlashesAndToolRowSummary(t *testing.T) {
	t.Skip("build banner is being reimplemented as a core-ui/widget Banner preset; restore this test once that lands")
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Land on the host page and wait for SSE to be live.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatalf("SSE never opened")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Banner exists in DOM but starts hidden.
	var hasBanner, bannerOn bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`!!document.getElementById("kiln-build-banner")`, &hasBanner),
		chromedp.Evaluate(`document.getElementById("kiln-build-banner").classList.contains("kiln-build-banner-on")`, &bannerOn),
	); err != nil {
		t.Fatalf("read initial banner state: %v", err)
	}
	if !hasBanner {
		t.Fatalf("build banner not in DOM")
	}
	if bannerOn {
		t.Fatalf("banner already active before any edit")
	}

	// Trigger an external add_entity — SSE should flash the banner.
	res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "tickets",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "priority", Type: "int"},
		},
	}})
	if !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}

	// Poll for the banner to turn on (SSE round-trip + flashBuildBanner).
	flashCtx, flashCancel := context.WithTimeout(ctx, 5*time.Second)
	defer flashCancel()
	var label string
	for {
		var on bool
		_ = chromedp.Run(flashCtx,
			chromedp.Evaluate(`document.getElementById("kiln-build-banner").classList.contains("kiln-build-banner-on")`, &on),
			chromedp.Evaluate(`document.getElementById("kiln-build-label").textContent || ""`, &label),
		)
		if on {
			break
		}
		if flashCtx.Err() != nil {
			t.Fatalf("banner never flashed; last label=%q", label)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Mid-flash label is either "applying add_entity…" or "agent is building…"
	if !strings.Contains(label, "add_entity") && !strings.Contains(label, "building") {
		t.Errorf("banner label unexpected mid-flash: %q", label)
	}

	// Tool-call row in the panel should render the summarized args, not raw JSON.
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`.kiln-msg-tool`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("waiting for tool row: %v", err)
	}
	var rowText string
	if err := chromedp.Run(ctx,
		chromedp.Text(`.kiln-msg-tool`, &rowText, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("read tool row: %v", err)
	}
	// The synthetic system row injected by world_edit reads "✦ add_entity";
	// the *summarized* row is the call entry from the journal which uses
	// summarizeArgs(args) — name=tickets fields=2.
	allRows := []string{}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`Array.from(document.querySelectorAll(".kiln-msg-tool")).map(el=>el.textContent)`, &allRows),
	); err != nil {
		t.Fatalf("read all tool rows: %v", err)
	}
	joined := strings.Join(allRows, "\n")
	for _, want := range []string{"name=tickets", "fields=2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tool rows missing summarized arg %q in:\n%s", want, joined)
		}
	}

	// Banner should auto-clear within ~2s after the flash window expires.
	clearCtx, clearCancel := context.WithTimeout(ctx, 4*time.Second)
	defer clearCancel()
	for {
		var on bool
		_ = chromedp.Run(clearCtx,
			chromedp.Evaluate(`document.getElementById("kiln-build-banner").classList.contains("kiln-build-banner-on")`, &on),
		)
		if !on {
			break
		}
		if clearCtx.Err() != nil {
			t.Fatalf("banner never auto-cleared")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// --- (9) plan approve button ----------------------------------------
//
// Verifies the user-facing safety loop: when an agent proposes a plan,
// the panel renders Approve/Reject buttons; clicking Approve calls
// approve_plan which marks the plan approved in the journal, and the
// gated destructive op succeeds when retried with the plan_id.
func TestBrowser_ApprovePlanButton(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Seed: an entity to delete + an agent-proposed plan covering it.
	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "trash", Fields: []world.Field{{Name: "x", Type: "string"}},
	}}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID:  "p1",
		Steps:   []string{"drop trash"},
		Reason:  "user wants to clean up",
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "trash"}},
	}); !r.OK {
		t.Fatal(r)
	}

	// Open the panel and wait for the plan card.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Wait for the plan card to render via SSE → refresh.
	deadline := time.Now().Add(8 * time.Second)
	var planText string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`(()=>{const el=document.querySelector(".kiln-msg-plan");return el?el.textContent:""})()`, &planText),
		); err == nil && strings.Contains(planText, "p1") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(planText, "p1") {
		t.Fatalf("plan card never rendered; got %q", planText)
	}
	for _, want := range []string{"drop trash", "delete_entity trash", "Approve", "Reject"} {
		if !strings.Contains(planText, want) {
			t.Errorf("plan card missing %q in:\n%s", want, planText)
		}
	}

	// Click Approve.
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-plan-action="approve"][data-plan-id="p1"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click approve: %v", err)
	}

	// Plan should journal as approved — poll session.Plans.
	approvedDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(approvedDeadline) {
		plans := tools.Live().Session().Plans
		if p, ok := plans["p1"]; ok && p.Approved {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if p, ok := tools.Live().Session().Plans["p1"]; !ok || !p.Approved {
		t.Fatalf("plan p1 not approved in journal after click; plans=%+v",
			tools.Live().Session().Plans)
	}

	// And the destructive op must now succeed when called with plan_id.
	res := tools.DeleteEntity(t.Context(), protocol.DeleteEntityArgs{Name: "trash", PlanID: "p1"})
	if !res.OK {
		t.Fatalf("delete_entity with approved plan failed: %+v", res)
	}
	if _, exists := tools.Live().Session().World.Entities["trash"]; exists {
		t.Error("entity still present after approved delete")
	}

	// Re-render the plan card; it should now show Approved status.
	var statusText string
	statusDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(statusDeadline) {
		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`(()=>{const el=document.querySelector(".kiln-plan-status-approved");return el?el.textContent:""})()`, &statusText),
		)
		if strings.Contains(statusText, "Approved") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(statusText, "Approved") {
		t.Errorf("plan card never showed Approved status; got %q", statusText)
	}
}

// --- (10) HTTP dispatch journals tool_call/tool_result -------------
//
// The HTTP /kiln/tool/{name} dispatcher wraps each call in a tool_call
// envelope and follows up with a tool_result. The widget renders these
// as → / ← rows in the panel using summarizeArgs. This test invokes
// the HTTP path directly (not the typed protocol) and verifies both
// rows show up with the expected text.
func TestBrowser_HTTPDispatchJournalsToolCallAndResult(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatalf("SSE never opened")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Hit the HTTP dispatcher directly with an add_entity call.
	body := strings.NewReader(`{"entity":{"name":"items","fields":[{"name":"label","type":"string"},{"name":"qty","type":"int"}]}}`)
	resp, err := http.Post(urlBase+"/kiln/tool/add_entity", "application/json", body)
	if err != nil {
		t.Fatalf("POST /kiln/tool/add_entity: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	// Wait for both rows to land via SSE → refresh.
	deadline := time.Now().Add(5 * time.Second)
	var rows []string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`Array.from(document.querySelectorAll(".kiln-msg-tool, .kiln-msg-tool-error")).map(el=>el.textContent)`, &rows),
		)
		if len(rows) >= 2 && containsAny(rows, "▢ add_entity") && containsAny(rows, "← ok") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"▢ add_entity", "name=items", "fields=2", "← ok"} {
		if !strings.Contains(joined, want) {
			t.Errorf("panel rows missing %q in:\n%s", want, joined)
		}
	}
}

func containsAny(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// --- (11) Reset session button --------------------------------------
//
// Verifies the panel's Reset button: builds an entity, clicks Reset
// (auto-accepting the window.confirm dialog), and verifies the world
// is empty afterwards.
func TestBrowser_ResetSessionButton(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Seed: an entity that should disappear after reset.
	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "trash", Fields: []world.Field{{Name: "x", Type: "string"}},
	}}); !r.OK {
		t.Fatal(r)
	}
	if _, ok := tools.Live().Session().World.Entities["trash"]; !ok {
		t.Fatal("seed entity didn't land before reset")
	}

	// Open panel, click Reset → confirmation modal opens, click the
	// danger Reset button inside the modal to confirm.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`#kiln-reset`, chromedp.ByQuery),
		chromedp.Click(`#kiln-reset`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal-danger`, chromedp.ByQuery),
		chromedp.Click(`.kiln-modal-danger`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click reset: %v", err)
	}

	// Wait for the journal to be wiped — poll session state.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(tools.Live().Session().World.Entities) == 0 &&
			len(tools.Live().Session().Chat) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := len(tools.Live().Session().World.Entities); got != 0 {
		t.Errorf("entities not cleared by reset; got %d", got)
	}
	if got := len(tools.Live().Session().Chat); got != 0 {
		t.Errorf("chat not cleared by reset; got %d", got)
	}
}

// --- (12) Agent settings modal --------------------------------------
//
// The kiln chat server in startKiln doesn't wire the runtime adapter
// store (that lives in cmd/kiln). For the widget side, we just verify
// the modal opens and renders the correct chrome — backend coverage of
// /kiln/agent is in cmd/kiln tests. This catches the click-handler →
// modal-render → buttons-present chain.
func TestBrowser_AgentConfigModalOpens(t *testing.T) {
	t.Skip("config modal is being reimplemented as a core-ui/widget Modal preset; restore once that lands")
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`#kiln-config`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// /kiln/agent isn't mounted in startKiln (no adapter store), so the
	// modal would error out talking to it. We stub the JSON fetcher in
	// the page so the modal can still render — this test checks the
	// click-handler + DOM assembly path, not the backend round-trip.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{
			window.__origFetch = window.fetch;
			window.fetch = function(u, opts) {
				if (typeof u === "string" && u.indexOf("/kiln/agent") === 0) {
					return Promise.resolve(new Response(JSON.stringify({
						current: { name: "claude-code", display: "claude --print …" },
						available: [
							{ name: "claude-code", display: "claude", installed: true },
							{ name: "pi", display: "pi -p …", installed: false },
							{ name: "codex", display: "codex exec", installed: false },
						],
						order: ["claude-code","pi","codex"],
						in_flight: true,
					}), { status: 200, headers: {"Content-Type":"application/json"} }));
				}
				return window.__origFetch(u, opts);
			};
		})()`, nil),
		chromedp.Click(`#kiln-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open modal: %v", err)
	}

	// Inspect the modal's contents.
	var html string
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML(`.kiln-modal`, &html, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("read modal html: %v", err)
	}
	for _, want := range []string{
		"Agent settings",
		"claude-code", "pi", "codex",
		"none", "custom",
		"Apply", "Cancel",
		"A turn is running", // in_flight warning
	} {
		if !strings.Contains(html, want) {
			t.Errorf("modal missing %q in:\n%s", want, html[:min(len(html), 600)])
		}
	}
}

// --- (13) New core-ui/widget-driven panel mounts cleanly ----------
//
// Exercises chat.MountPanel — the framework-driven kiln panel. The
// runtime is now served at /__gofastr/runtime.js and auto-discovers
// every registered widget via /__gofastr/widgets.
func TestBrowser_NewPanelMountsViaWidget(t *testing.T) {
	urlBase, _ := startKiln(t)

	// Shared framework runtime — single URL, idempotent IIFE, fetches
	// the widget list at startup.
	resp, err := http.Get(urlBase + "/__gofastr/runtime.js")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("framework runtime not reachable: status=%d err=%v", resp.StatusCode, err)
	}
	rtBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{"window.__gofastr", "mountWidget", "/__gofastr/widgets"} {
		if !strings.Contains(string(rtBody), want) {
			t.Errorf("runtime missing %q", want)
		}
	}

	// Widget discovery — runtime fetches this; one entry per registered
	// widget, with cfg + chrome HTML inline.
	resp, err = http.Get(urlBase + "/__gofastr/widgets")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("widget discovery not reachable: status=%d err=%v", resp.StatusCode, err)
	}
	listBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{
		`"name":"kiln-panel"`,
		`"signal":"chat_html"`,
		`kiln-log-wrap`,
		`kiln-input`,
		`/kiln/panel/send`,
	} {
		if !strings.Contains(string(listBody), want) {
			t.Errorf("widget discovery list missing %q", want)
		}
	}

	// Per-widget /state still serves the signal snapshot.
	resp, err = http.Get(urlBase + "/core-ui/widget/kiln-panel/state")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("new panel state not reachable: status=%d err=%v", resp.StatusCode, err)
	}
	stateBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(stateBody), "chat_html") {
		t.Errorf("new panel state missing chat_html signal: %s", string(stateBody))
	}

	// Per-widget stylesheet still serves the theme-resolved CSS.
	resp, err = http.Get(urlBase + "/core-ui/widget/kiln-panel/style.css")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("new panel style not reachable: status=%d err=%v", resp.StatusCode, err)
	}
	cssBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{":root", ".fui-widget", ".fui-pos-bottom-right"} {
		if !strings.Contains(string(cssBody), want) {
			t.Errorf("new panel style missing %q", want)
		}
	}
}

// startKiln currently doesn't wire chat.MountPanel because the test
// helper predates it. Add a fixture that does, and use it in the
// migration test only.
func startKilnWithNewPanel(t *testing.T) (string, *protocol.Tools) {
	t.Helper()
	// Reuse startKiln to get the Live + Tools, then mount the new panel
	// onto the same router. We can't easily reach into startKiln's
	// internals; for the sanity test above we just rely on the legacy
	// routes being present and verify panel routes existed if mounted
	// elsewhere. To make the test deterministic, we run this end-to-end
	// in a separate fixture below.
	url, tools := startKiln(t)
	return url, tools
}

// --- (14) Panel error path doesn't poison the log -------------------
//
// Empty-send returns 400. The legacy bug: the runtime captured the
// non-OK response into the chat_html signal which then rendered the
// JSON error blob as innerHTML of the log. Tests added here BEFORE
// the fix; they fail until panel.go drops its chat_html signal bind
// and lets the SSE refetch own log updates instead.
func TestBrowser_EmptySendDoesNotPoisonLog(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-send`, chromedp.ByQuery),
		// Click Send with an empty input — fires real submit event.
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("empty submit: %v", err)
	}

	var logHTML string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.kiln-log')?.innerHTML ?? ''`, &logHTML),
	); err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`"ok":false`, `"status":400`, `"text":"empty`} {
		if strings.Contains(logHTML, banned) {
			t.Errorf("log was polluted by error response — found %q in:\n%s", banned, logHTML)
		}
	}
}

// TestBrowser_SendMessageUpdatesLogViaSSE: legitimate send must end up
// in the log. With the chat_html bind removed, the SSE refetch is the
// only path; this guards against the regression where dropping the
// bind also drops legit updates.
func TestBrowser_SendMessageUpdatesLogViaSSE(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		chromedp.SendKeys(`.kiln-input`, "hello via panel"),
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var logHTML string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('.kiln-log')?.innerHTML ?? ''`, &logHTML),
		)
		if strings.Contains(logHTML, "hello via panel") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(logHTML, "hello via panel") {
		t.Errorf("message never appeared in log via SSE refetch:\n%s", logHTML)
	}
	// And the journal recorded it (sanity).
	chats := tools.Live().Session().Chat
	if len(chats) == 0 || chats[len(chats)-1].Message == nil ||
		!strings.Contains(chats[len(chats)-1].Message.Text, "hello via panel") {
		t.Errorf("journal didn't capture the message: %+v", chats)
	}
}

// TestBrowser_GearOpenedModalIsActuallyVisible: opening the modal isn't
// enough — the user has to actually see a styled card. Catches the case
// where the framework chrome mounts but the host's slot CSS classes
// aren't loaded, leaving a transparent modal that looks like nothing.
func TestBrowser_GearOpenedModalIsActuallyVisible(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-fui-widget="kiln-agent-settings"]`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("open modal: %v", err)
	}

	var diag string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{
			const card = document.querySelector('[data-fui-widget="kiln-agent-settings"] .kiln-modal');
			if (!card) return JSON.stringify({error: "modal card not in DOM"});
			const cs = getComputedStyle(card);
			const r = card.getBoundingClientRect();
			return JSON.stringify({
				bg: cs.backgroundColor,
				borderRadius: cs.borderRadius,
				padding: cs.padding,
				width: r.width, height: r.height,
			});
		})()`, &diag),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("modal card style: %s", diag)
	// A styled card has a non-transparent background and non-zero size.
	for _, want := range []string{`"bg":"rgba(`, `"borderRadius":"`} {
		if !strings.Contains(diag, want) {
			t.Errorf("modal card not visibly styled: %s", diag)
		}
	}
	// Background should NOT be the default transparent (rgba(0,0,0,0)).
	if strings.Contains(diag, `"bg":"rgba(0, 0, 0, 0)"`) {
		t.Errorf("modal card background is fully transparent — host CSS not loaded into modal widget: %s", diag)
	}
}

// TestBrowser_GearOpensAgentSettingsModal: clicking the gear button
// (data-fui-open="kiln-agent-settings") mounts the previously-hidden
// Modal widget. Catches the "I can't even open the gear" regression.
func TestBrowser_GearOpensAgentSettingsModal(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Modal should NOT be visible before the click.
	var presentBefore bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="kiln-agent-settings"]')`,
			&presentBefore,
		),
	); err != nil {
		t.Fatalf("pre-click: %v", err)
	}
	if presentBefore {
		t.Errorf("agent-settings modal should be hidden before gear click")
	}

	// Click gear, modal mounts.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click gear: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="kiln-agent-settings"]')`,
			&present))
		if present {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Errorf("agent-settings modal never appeared after gear click")
}

// TestBrowser_GearOpenedModalListsAgents: the modal must actually show
// the agent CLI rows after opening — not just the styled card. Catches
// the "Loading…" placeholder never being replaced (Signal not wired or
// no provider passed to MountPanel).
func TestBrowser_GearOpenedModalListsAgents(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-fui-widget="kiln-agent-settings"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("open modal: %v", err)
	}

	// Wait for the agent list to populate (Loading… → adapter rows).
	deadline := time.Now().Add(3 * time.Second)
	var diag string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(()=>{
			const list = document.querySelector('#kiln-agent-list');
			if (!list) return JSON.stringify({error:"list not in DOM"});
			const rows = list.querySelectorAll('.kiln-adapter-row');
			const txt = list.textContent.trim();
			return JSON.stringify({rows: rows.length, text: txt.slice(0,160)});
		})()`, &diag))
		if strings.Contains(diag, `"rows":`) && !strings.Contains(diag, `"rows":0`) {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Logf("agent list: %s", diag)
	if strings.Contains(diag, `Loading…`) {
		t.Errorf("agent list still showing Loading… placeholder — Signal didn't hydrate: %s", diag)
	}
	if !strings.Contains(diag, `"rows":`) || strings.Contains(diag, `"rows":0`) {
		t.Errorf("agent list rendered no .kiln-adapter-row entries: %s", diag)
	}
}

// TestBrowser_ApplyAgentActuallyPosts: clicking a radio + Apply must
// fire a POST /kiln/agent with {name: "<selected>"}. Catches the case
// where the form submit handler is overshadowed by the click handler
// (or vice versa), the FormData serialization drops the radio, or the
// runtime treats the form's data-fui-rpc as a click target only.
func TestBrowser_ApplyAgentActuallyPosts(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Stub /kiln/agent to capture the POST body without going through
	// the real adapter store.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`#kiln-agent-list .kiln-adapter-row`, chromedp.ByQuery),
		// Install a fetch interceptor that records the POST body.
		chromedp.Evaluate(`(()=>{
			window.__capturedAgentPost = null;
			const orig = window.fetch;
			window.fetch = function(input, init) {
				try {
					const url = typeof input === 'string' ? input : (input && input.url) || '';
					if (url.indexOf('/kiln/agent') >= 0 && init && init.method === 'POST') {
						window.__capturedAgentPost = init.body;
					}
				} catch(_) {}
				return orig.apply(this, arguments);
			};
			return true;
		})()`, nil),
		chromedp.Click(`#kiln-agent-list input[value="claude-code"]`, chromedp.ByQuery),
		chromedp.Click(`#kiln-agent-list .kiln-modal-apply`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("apply flow: %v", err)
	}

	var captured string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__capturedAgentPost || ""`, &captured),
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured POST body: %q", captured)
	if captured == "" {
		t.Fatalf("apply click did not POST to /kiln/agent — no fetch captured")
	}
	if !strings.Contains(captured, `"name":"claude-code"`) {
		t.Errorf("captured body missing selected adapter: %q", captured)
	}
}

// TestBrowser_ApplyAgentClosesModal: after a successful Apply POST,
// the modal must dismiss so the user has visible feedback that the
// click landed. Without this the click looks like a no-op (POST fires
// in the background but the modal stays open and unchanged).
func TestBrowser_ApplyAgentClosesModal(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`#kiln-agent-list .kiln-adapter-row`, chromedp.ByQuery),
		chromedp.Click(`#kiln-agent-list input[value="claude-code"]`, chromedp.ByQuery),
		chromedp.Click(`#kiln-agent-list .kiln-modal-apply`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("apply flow: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="kiln-agent-settings"]')`,
			&present))
		if !present {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	t.Errorf("modal did not dismiss after Apply — user gets no visible feedback that the click landed")
}

// After a successful chat send the textarea should clear so the next
// keystroke starts a fresh prompt. Otherwise pressing Send again
// resubmits the same text — surprising and dangerous (re-fires the
// agent on the same prompt). The fix is framework-level: the
// data-fui-rpc-reset opt-in on the form tells the runtime to call
// form.reset() after a 2xx ack.
func TestBrowser_SendClearsInput(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		chromedp.SendKeys(`.kiln-input`, "a one-shot prompt"),
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("send flow: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var val string
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelector('.kiln-input').value`, &val))
		if val == "" {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	var lastVal string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('.kiln-input').value`, &lastVal))
	t.Errorf("textarea did not clear after Send — risk of accidental resubmit. value=%q", lastVal)
}

// When the agent watcher fires "agent_turn_started", the panel header
// should show a visible "agent thinking…" indicator within 2s. When
// "agent_turn_ended" fires, the indicator should disappear within 2s.
// Drives the synthetic SSE events via Live.Notify (which the watcher
// uses in production) so the test exercises the same path without
// spawning a real agent subprocess.
func TestBrowser_AgentTurnInFlightShowsStatus(t *testing.T) {
	urlBase, l, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-status`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatalf("SSE never opened")
		}
		time.Sleep(80 * time.Millisecond)
	}

	// Indicator should be empty before any turn starts.
	var pre string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &pre, chromedp.ByQuery))
	if strings.Contains(pre, "thinking") {
		t.Fatalf("status said %q before any turn started", pre)
	}

	// Simulate a turn starting.
	testInFlight.Store(true)
	l.Notify("agent_turn_started", "pi")

	deadline := time.Now().Add(2 * time.Second)
	var seen string
	for time.Now().Before(deadline) {
		var s string
		_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &s, chromedp.ByQuery))
		if strings.Contains(s, "thinking") {
			seen = s
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !strings.Contains(seen, "thinking") {
		t.Fatalf("indicator never appeared after agent_turn_started; status=%q", seen)
	}

	// Simulate the turn ending.
	testInFlight.Store(false)
	l.Notify("agent_turn_ended", "pi")

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var s string
		_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &s, chromedp.ByQuery))
		if !strings.Contains(s, "thinking") {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var last string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &last, chromedp.ByQuery))
	t.Errorf("indicator did not clear after agent_turn_ended; status=%q", last)
}

// Clicking ↺ (Reset) should immediately clear the panel chat list,
// not wait for the next unrelated SSE event. ResetSession truncates
// the journal and reloads on the backend, but until something else
// fired chat_html refresh the panel kept showing stale items.
func TestBrowser_ResetClearsPanelImmediately(t *testing.T) {
	urlBase, _, tools := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Seed a chat message so the log isn't empty before reset.
	tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "seeded prompt"})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
		chromedp.Click(`#kiln-reset`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal-danger`, chromedp.ByQuery),
		chromedp.Click(`.kiln-modal-danger`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("reset flow: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('.kiln-msg-user')`, &present))
		if !present {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Errorf("panel chat list still showed seeded message 2s after Reset — UI did not react to session_reset")
}

// The empty-state landing page shows a curl example. Previously the
// example hardcoded http://localhost:8765 — wrong on any non-default
// port. Should render the actual server origin so users can copy the
// example directly into their terminal.
func TestBrowser_LandingPageCurlUsesActualHost(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)

	resp, err := http.Get(urlBase + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if strings.Contains(page, "http://localhost:8765") {
		t.Errorf("landing page still hardcodes http://localhost:8765 — should use actual host %q", urlBase)
	}
	if !strings.Contains(page, urlBase) {
		t.Errorf("landing page does not contain actual host %q in any curl example", urlBase)
	}
}

// Verifies the runtime's data-fui-scroll-bottom-on-update opt-in:
// after setSignal updates an html-mode signal node that has the
// attribute, the resolved target's scrollTop should be at the bottom.
// Builds a controlled overflow container in JS so the test isn't at
// the mercy of the kiln panel's flex layout.
func TestBrowser_RuntimeScrollBottomOnUpdate(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, 6*time.Second)
	defer pollCancel()
	for {
		var ok bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(
			`!!(window.__gofastr && window.__gofastr.setSignal)`, &ok)); err == nil && ok {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatal("runtime namespace never loaded")
		}
		time.Sleep(60 * time.Millisecond)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		(function(){
			const c = document.createElement('div');
			c.id = 'scroll-test-container';
			c.setAttribute('data-fui-signal', 'scroll_test');
			c.setAttribute('data-fui-signal-mode', 'html');
			c.setAttribute('data-fui-scroll-bottom-on-update', '');
			c.style.cssText = 'position:fixed;top:0;left:0;width:200px;height:60px;overflow:auto;border:1px solid red;background:white;z-index:99999;';
			c.innerHTML = '<div style="height:200px">initial</div>';
			document.body.appendChild(c);
			c.scrollTop = 0;
		})()
	`, nil)); err != nil {
		t.Fatal(err)
	}

	var preTop, preHeight, preClient float64
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('scroll-test-container').scrollTop`, &preTop))
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('scroll-test-container').scrollHeight`, &preHeight))
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('scroll-test-container').clientHeight`, &preClient))
	if preTop != 0 || preHeight <= preClient+4 {
		t.Fatalf("setup: container not in expected state (top=%v height=%v client=%v)", preTop, preHeight, preClient)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.setSignal('scroll_test', '<div style="height:300px">replaced and taller</div>')`, nil)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var top, height, client float64
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.getElementById('scroll-test-container').scrollTop`, &top))
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.getElementById('scroll-test-container').scrollHeight`, &height))
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.getElementById('scroll-test-container').clientHeight`, &client))
		if top > 0 && top+client >= height-4 {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	var dump string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify({top:document.getElementById('scroll-test-container').scrollTop,h:document.getElementById('scroll-test-container').scrollHeight,c:document.getElementById('scroll-test-container').clientHeight})`,
		&dump))
	t.Errorf("scroll-bottom-on-update did not pin scrollTop to bottom: %s", dump)
}

// And the kiln chat panel must opt in via the attribute on its log
// container — otherwise even a working runtime feature won't help
// users in the actual chat UI. The framework serves the panel's
// chrome HTML via /__gofastr/widgets; assert the attribute is in
// that payload.
func TestKilnPanelOptsIntoAutoScroll(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	resp, err := http.Get(urlBase + "/__gofastr/widgets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, `data-fui-scroll-bottom-on-update`) {
		t.Errorf("kiln chat panel does not declare data-fui-scroll-bottom-on-update on its log container")
	}
}

// World snapshot pill in the panel header keeps the user oriented:
// 'empty world' on a fresh start; '1 entity' / '2 entities · 1 page'
// after the agent works. Updates live via SSE refresh on world_edit.
func TestBrowser_WorldSnapshotPillReflectsLiveWorldChanges(t *testing.T) {
	urlBase, _, tools := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-snapshot`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Initial state: empty world.
	var initial string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-snapshot`, &initial, chromedp.ByQuery))
	if !strings.Contains(initial, "empty") {
		t.Fatalf("expected 'empty world' on fresh load, got %q", initial)
	}

	// Wait for SSE so the world_edit refetch can fire.
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatal("SSE never opened")
		}
		time.Sleep(80 * time.Millisecond)
	}

	// Add an entity — pill should re-render to "1 entity" within 2s.
	res := tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "notes", Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		}},
	})
	if !res.OK {
		t.Fatalf("add_entity failed: %v", res.Error)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var pill string
		_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-snapshot`, &pill, chromedp.ByQuery))
		if strings.Contains(pill, "1 entity") {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var last string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-snapshot`, &last, chromedp.ByQuery))
	t.Errorf("snapshot pill never updated to '1 entity' after add_entity; final=%q", last)
}

// Reset is destructive (truncates journal + drops DB schema). The
// header ↺ button must NOT directly reset — it must open a confirm
// modal. Cancel from the modal preserves the world; Confirm wipes.
func TestBrowser_ResetButtonAsksForConfirmation(t *testing.T) {
	urlBase, _, tools := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Seed a chat message — should survive a Cancel and disappear on Confirm.
	tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "do not lose me"})

	// Click ↺ → modal opens with Cancel + Reset buttons.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
		chromedp.Click(`#kiln-reset`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal-danger`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open confirm modal: %v", err)
	}

	// Modal must contain a clearly destructive Reset button distinct from Cancel.
	var hasDanger, hasCancel bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('.kiln-modal-danger')`, &hasDanger))
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('.kiln-modal-cancel')`, &hasCancel))
	if !hasDanger || !hasCancel {
		t.Fatalf("confirm modal missing Cancel/Reset buttons: danger=%v cancel=%v", hasDanger, hasCancel)
	}

	// Cancel: chat message should still be there.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.kiln-modal-cancel`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	var msgPresent bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('.kiln-msg-user')`, &msgPresent))
	if !msgPresent {
		t.Errorf("chat message disappeared after Cancel — confirm modal triggered the reset anyway")
	}

	// Confirm: re-open and click the danger button → message should disappear.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#kiln-reset`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal-danger`, chromedp.ByQuery),
		chromedp.Click(`.kiln-modal-danger`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("confirm flow: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('.kiln-msg-user')`, &present))
		if !present {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Errorf("chat message did not clear after Confirm — confirm flow broken")
}

// Send button stays disabled while the textarea is empty so the user
// can't accidentally fire a no-op POST. Becomes enabled the moment
// any non-whitespace text is typed; goes back to disabled after the
// 2xx ack clears the textarea (existing data-fui-rpc-reset).
func TestBrowser_SendButtonDisabledWhileInputEmpty(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-send`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// Initial state: empty textarea → button must be disabled.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var disabled bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-send').disabled`, &disabled))
		if disabled {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var initialDisabled bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-send').disabled`, &initialDisabled))
	if !initialDisabled {
		t.Fatalf("Send button enabled with empty input — should be disabled")
	}

	// Type → button enables.
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`.kiln-input`, "hello"),
	); err != nil {
		t.Fatal(err)
	}
	var afterTypeDisabled bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-send').disabled`, &afterTypeDisabled))
	if afterTypeDisabled {
		t.Errorf("Send button still disabled after typing — should be enabled")
	}

	// Send → input clears via data-fui-rpc-reset → button disables again.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var disabled bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-send').disabled`, &disabled))
		if disabled {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("Send button stayed enabled after send cleared the input")
}

// Esc closes any open modal — keyboard users shouldn't have to mouse
// over to Cancel. preset.Modal sets CloseOnEscape, the runtime wires
// the keydown listener; this test pins that wiring against
// regressions for both kiln modals (gear + reset-confirm).
func TestBrowser_EscClosesModals(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-config`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// (a) Gear modal
	if err := chromedp.Run(ctx,
		chromedp.Click(`.kiln-panel-config`, chromedp.ByQuery),
		chromedp.WaitVisible(`#kiln-agent-list`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open gear: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape)); err != nil { // ESC
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="kiln-agent-settings"]')`, &present))
		if !present {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	var stillGear bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('[data-fui-widget="kiln-agent-settings"]')`, &stillGear))
	if stillGear {
		t.Errorf("gear modal still present after Esc")
	}

	// (b) Reset-confirm modal
	if err := chromedp.Run(ctx,
		chromedp.Click(`#kiln-reset`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-modal-danger`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
	); err != nil {
		t.Fatalf("open + esc reset: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-fui-widget="kiln-reset-confirm"]')`, &present))
		if !present {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	t.Errorf("reset-confirm modal still present after Esc")
}

// Tool-call rows annotate elapsed time (or "running…" while pending),
// and tool_result rows echo the tool name. Long agent turns become
// scannable: "▢ add_entity name=foo (210ms)" / "← ok · add_entity".
func TestBrowser_ToolCallShowsElapsedTimeAndResultEchosName(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// Hit /kiln/tool/add_entity directly so the chat server journals
	// both tool_call AND tool_result with paired call IDs (the same
	// path pi takes when the agent dispatches a tool over HTTP).
	body := `{"entity":{"name":"notes","fields":[{"name":"title","type":"string","required":true}]}}`
	resp, err := http.Post(urlBase+"/kiln/tool/add_entity", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var rows []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('.kiln-msg-tool, .kiln-msg-tool-error')).map(el=>el.textContent)`,
			&rows))
		var sawElapsed, sawNameEcho bool
		for _, r := range rows {
			if strings.Contains(r, "▢ add_entity") && (strings.Contains(r, "ms)") || strings.Contains(r, "<1ms") || strings.Contains(r, "s)")) {
				sawElapsed = true
			}
			if strings.Contains(r, "← ok · add_entity") {
				sawNameEcho = true
			}
		}
		if sawElapsed && sawNameEcho {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var rows []string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.kiln-msg-tool, .kiln-msg-tool-error')).map(el=>el.textContent)`,
		&rows))
	t.Errorf("missing elapsed-time and/or name-echo annotations; rows=%v", rows)
}

// Pressing Enter (no Shift) inside the chat textarea submits the
// form — standard chat UX. Shift+Enter still inserts a newline.
func TestBrowser_EnterSubmitsChat(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		chromedp.SendKeys(`.kiln-input`, "hi via enter"),
		chromedp.Focus(`.kiln-input`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Enter),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("enter-submit flow: %v", err)
	}
	// Input should have cleared (data-fui-rpc-reset).
	var val string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-input').value`, &val))
		if val == "" {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	if val != "" {
		t.Errorf("textarea did not clear after Enter-submit: %q", val)
	}

	// Shift+Enter must NOT submit — it inserts a newline.
	if err := chromedp.Run(ctx,
		chromedp.Focus(`.kiln-input`, chromedp.ByQuery),
		chromedp.SendKeys(`.kiln-input`, "line1"),
		chromedp.KeyEvent(kb.Enter, chromedp.KeyModifiers(8)), // 8 = Shift per cdproto/input.Modifier
		chromedp.SendKeys(`.kiln-input`, "line2"),
	); err != nil {
		t.Fatalf("shift+enter setup: %v", err)
	}
	var afterShift string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.kiln-input').value`, &afterShift))
	if !strings.Contains(afterShift, "line1") || !strings.Contains(afterShift, "line2") {
		t.Errorf("Shift+Enter should have inserted a newline; textarea=%q", afterShift)
	}
}

// In-flight indicator includes a per-turn tool counter that ticks up
// as the agent dispatches tools: 'agent thinking · 1 tool', '… · 3
// tools'. Counted from the most-recent chat_user message.
func TestBrowser_InFlightCountsToolCalls(t *testing.T) {
	urlBase, l, tools := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-status`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatal("SSE never opened")
		}
		time.Sleep(80 * time.Millisecond)
	}

	// Simulate a turn in flight + journal a user msg + dispatch tools.
	tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "build something"})
	testInFlight.Store(true)
	l.Notify("agent_turn_started", "pi")

	// Two tool dispatches via the chat HTTP path so tool_call SSE fires.
	for _, body := range []string{
		`{"entity":{"name":"a","fields":[{"name":"x","type":"string"}]}}`,
		`{"entity":{"name":"b","fields":[{"name":"y","type":"string"}]}}`,
	} {
		resp, err := http.Post(urlBase+"/kiln/tool/add_entity", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &status, chromedp.ByQuery))
		if strings.Contains(status, "thinking") && strings.Contains(status, "2 tools") {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var last string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-status`, &last, chromedp.ByQuery))
	t.Errorf("status never showed '2 tools'; final=%q", last)
}

// Failed tool dispatches surface as a distinct error row: ✗ prefix
// (not the success ←), kiln-msg-tool-error class with red treatment,
// tool name + error reason on the same line. Hits a known-bad
// add_entity payload (missing required name) to force a validation
// error from the protocol layer.
func TestBrowser_FailedToolDispatchSurfacesDistinctRow(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// Bad add_entity: empty name field violates required.
	body := `{"entity":{"name":"","fields":[{"name":"x","type":"string"}]}}`
	resp, err := http.Post(urlBase+"/kiln/tool/add_entity", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var rows []string
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('.kiln-msg-tool-error')).map(el=>el.textContent)`,
			&rows))
		var sawErrorRow bool
		for _, r := range rows {
			if strings.HasPrefix(strings.TrimSpace(r), "✗") {
				sawErrorRow = true
				break
			}
		}
		if sawErrorRow {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var allRows []string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.kiln-msg-tool, .kiln-msg-tool-error')).map(el=>el.className+': '+el.textContent)`,
		&allRows))
	t.Errorf("did not find a kiln-msg-tool-error row prefixed with ✗; rows=%v", allRows)
}

// The empty-state landing page lead paragraph adapts to world content:
// 'Empty world…' on a fresh start, '2 entities · 1 page live…' once
// the agent has built things. Renders against the actual world via
// HostHTMLForLive (set up in startKilnExt).
func TestBrowser_LandingLeadAdaptsToWorld(t *testing.T) {
	urlBase, _, tools := startKilnExt(t)

	// Empty world: lead must say "Empty world".
	resp, err := http.Get(urlBase + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Empty world") {
		t.Errorf("expected 'Empty world' lead with no entities; got body fragment: %s", firstN(string(body), 800))
	}

	// Add two entities; lead should now reflect world content.
	tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "notes", Fields: []world.Field{{Name: "title", Type: "string", Required: true}}}})
	tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name: "users", Fields: []world.Field{{Name: "name", Type: "string"}}}})

	resp, err = http.Get(urlBase + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	if strings.Contains(page, "Empty world") {
		t.Errorf("lead still says 'Empty world' after adding entities")
	}
	if !strings.Contains(page, "2 entities") {
		t.Errorf("lead doesn't reflect entity count; body fragment: %s", firstN(page, 800))
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Long multi-line prompts make the textarea grow up to its CSS
// max-height, so the user sees what they're typing without manually
// resizing or scrolling inside the input.
func TestBrowser_TextareaAutoGrowsWithContent(t *testing.T) {
	urlBase, _, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// Capture the initial rendered height (rows=2).
	var initialHeight float64
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('.kiln-input').getBoundingClientRect().height`, &initialHeight))

	// Insert a long multi-line value and fire input.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		(function(){
			const ta = document.querySelector('.kiln-input');
			ta.value = 'line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8';
			ta.dispatchEvent(new Event('input', { bubbles: true }));
		})()
	`, nil)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var h float64
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`document.querySelector('.kiln-input').getBoundingClientRect().height`, &h))
		if h > initialHeight+8 {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	var finalH float64
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('.kiln-input').getBoundingClientRect().height`, &finalH))
	t.Errorf("textarea did not grow with content: initial=%v final=%v", initialHeight, finalH)
}

// User messages submitted from a kiln-built page carry a "[page=/foo] "
// prefix from the widget; the panel surfaces that prefix as a chip
// next to the message body so the agent's context isn't invisible.
func TestBrowser_PagePrefixRendersAsChip(t *testing.T) {
	urlBase, _, tools := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "[page=/dashboard] add a status field"})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var chipText string
		_ = chromedp.Run(ctx, chromedp.Evaluate(
			`(function(){const e=document.querySelector('.kiln-msg-user .kiln-msg-page');return e?e.textContent:'';})()`,
			&chipText))
		if chipText == "/dashboard" {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var fragment string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('.kiln-msg-user').outerHTML`, &fragment))
	t.Errorf("expected page chip '/dashboard' inside .kiln-msg-user; got: %s", fragment)
}

// Header agent chip reflects the current adapter and updates live
// when the user (or the API) switches via /kiln/agent — driven by
// the agent_changed SSE Notify.
func TestBrowser_AgentHeaderChipReflectsCurrentAndUpdates(t *testing.T) {
	urlBase, l, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-agent`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	var initial string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-agent`, &initial, chromedp.ByQuery))
	if initial != "no agent" {
		t.Errorf("expected initial chip 'no agent'; got %q", initial)
	}

	// Wait for SSE to be live so the refetch fires.
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			t.Fatal("SSE never opened")
		}
		time.Sleep(80 * time.Millisecond)
	}

	testCurrentAgent.Store("claude-code")
	l.Notify("agent_changed", "claude-code")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-agent`, &got, chromedp.ByQuery))
		if got == "claude-code" {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	var last string
	_ = chromedp.Run(ctx, chromedp.Text(`.kiln-panel-agent`, &last, chromedp.ByQuery))
	t.Errorf("agent chip never updated to 'claude-code'; final=%q", last)
}

// Pending tool rows get a live elapsed-time counter so a stuck tool
// is visible to the user without waiting for the result. Uses the
// runtime's data-fui-tick-elapsed primitive.
func TestBrowser_PendingToolRowTicksElapsedTime(t *testing.T) {
	urlBase, l, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Inject a tool_call directly into the journal WITHOUT a result so
	// the panel renders the pending state. Use a kind/op the journal
	// accepts via Apply.
	if err := l.Apply(journal.Entry{
		ID:        "pending-test-1",
		Timestamp: time.Now(),
		Kind:      journal.KindToolCall,
		Payload: mustJSON(journal.ToolCallPayload{
			CallID: "test-pending-1", Name: "add_entity",
			Args: map[string]any{"entity": map[string]any{"name": "x"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-msg-tool-pending`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// Wait for the first tick to replace the "…" placeholder.
	deadline := time.Now().Add(2 * time.Second)
	var t1 string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Text(`[data-fui-tick-elapsed]`, &t1, chromedp.ByQuery))
		if t1 != "…" && t1 != "" {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if t1 == "…" || t1 == "" {
		t.Fatalf("ticker never replaced placeholder; got %q", t1)
	}

	time.Sleep(700 * time.Millisecond)
	var t2 string
	_ = chromedp.Run(ctx, chromedp.Text(`[data-fui-tick-elapsed]`, &t2, chromedp.ByQuery))
	if t1 == t2 {
		t.Errorf("ticker did not advance over 700ms; t1=%q t2=%q", t1, t2)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// safety: keep fmt + journal imports live
var _ = fmt.Sprintf
var _ = journal.PlanTarget{}
var _ = startKilnWithNewPanel
