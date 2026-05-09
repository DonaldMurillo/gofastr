package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
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

// stand a live kiln server up on httptest. Same wiring as cmd/kiln.
func startKiln(t *testing.T) (string, *protocol.Tools) {
	t.Helper()
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
	l.SetFallbackHTML(chat.HostHTML())

	mcpSrv, err := kilnmcp.NewServer(tools)
	if err != nil {
		t.Fatal(err)
	}
	l.Aux().Handle("POST", "/mcp", mcpSrv)

	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	return srv.URL, tools
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
	for _, want := range []string{"kiln-fab", "kiln-panel-head", "kiln-input", "Send"} {
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
	// Wait for SSE to connect. EventSource sets __kilnSSEReady on open.
	pollCtx, pollCancel := context.WithTimeout(ctx, 8*time.Second)
	defer pollCancel()
	for {
		var ready bool
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__kilnSSEReady`, &ready)); err == nil && ready {
			break
		}
		if pollCtx.Err() != nil {
			var diag string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`(function(){
				return JSON.stringify({
					mounted: !!window.__kilnWidgetMounted,
					ready: !!window.__kilnSSEReady,
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
		if err := chromedp.Run(pollCtx, chromedp.Evaluate(`!!window.__kilnSSEReady`, &ready)); err == nil && ready {
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

// safety: keep fmt + journal imports live
var _ = fmt.Sprintf
var _ = journal.PlanTarget{}
