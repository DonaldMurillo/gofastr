package integration_test

import (
	"context"
	"github.com/DonaldMurillo/gofastr/internal/axetest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// screenshotDir is where we drop captured PNGs for human review.
func screenshotDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func saveShot(t *testing.T, name string, buf []byte) string {
	t.Helper()
	if len(buf) < 1000 {
		t.Errorf("%s: screenshot too small (%d bytes) — likely blank", name, len(buf))
	}
	path := filepath.Join(screenshotDir(t), name+".png")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// (1) Empty state on host renders the current framework floating panel and
// its server-rendered quick-start tray.
func TestVisual_HostEmptyState(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel.kiln-open`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-quickstart`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// The landing is themed by app.css tokens; capture it in both schemes.
	for _, scheme := range axetest.Schemes {
		var shot []byte
		if err := chromedp.Run(ctx,
			axetest.Prepare(scheme),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.FullScreenshot(&shot, 90),
		); err != nil {
			t.Fatalf("capture %s: %v", scheme, err)
		}
		path := saveShot(t, "01_host_empty_"+scheme, shot)
		t.Logf("saved: %s", path)
	}
}

// (2) Agent-turn state is visibly delivered by the current SSE signal path.
func TestVisual_StatusFeedbackDuringTurn(t *testing.T) {
	urlBase, l, _ := startKilnExt(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-panel-head`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}
	waitForPanelPoll(t, ctx)
	testInFlight.Store(true)
	l.Notify("agent_turn_started", "omp")

	var statusVisibleText string
	var shotPending []byte
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`.kiln-msg-thinking`, chromedp.ByQuery),
		chromedp.Text(`.kiln-msg-thinking`, &statusVisibleText, chromedp.ByQuery),
		chromedp.FullScreenshot(&shotPending, 90),
	); err != nil {
		t.Fatalf("pending: %v", err)
	}
	saveShot(t, "02_status_pending", shotPending)
	if !strings.Contains(statusVisibleText, "thinking") {
		t.Errorf("expected thinking status, got %q", statusVisibleText)
	}

	testInFlight.Store(false)
	l.Notify("agent_turn_ended", "omp")
	deadline := time.Now().Add(3 * time.Second)
	present := true
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('.kiln-msg-thinking')`, &present))
		if !present {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	if present {
		t.Error("thinking status remained after agent_turn_ended")
	}
	var shotOK []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shotOK, 90)); err != nil {
		t.Fatal(err)
	}
	saveShot(t, "03_status_after_send", shotOK)
}

// (3) After SSE delivers a world_edit, the system row is visibly stacked
// in the chat log alongside the user message.
func TestVisual_WorldEditSystemRow(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	waitForPanelPoll(t, ctx)

	// Type a user message to seat the panel.
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`.kiln-input`, "build me a posts entity"),
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-msg-user`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// External agent makes a change.
	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	}}); !r.OK {
		t.Fatal(r)
	}

	var shot []byte
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`.kiln-msg-tool`, chromedp.ByQuery),
		chromedp.FullScreenshot(&shot, 90),
	); err != nil {
		t.Fatal(err)
	}
	saveShot(t, "04_world_edit_system_row", shot)

	// Assert two distinct rows: user + tool.
	var rowCount int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('.kiln-msg').length`, &rowCount),
	); err != nil {
		t.Fatal(err)
	}
	if rowCount < 2 {
		t.Errorf("expected at least 2 rows in log, got %d", rowCount)
	}
}

// (4) Multiple rapid edits stack as a visible feed of system rows.
func TestVisual_RapidEditsAccumulate(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	waitForPanelPoll(t, ctx)

	// Fire 5 add_entity calls quickly.
	for i, name := range []string{"posts", "users", "comments", "tags", "drafts"} {
		r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
			Name:   name,
			Fields: []world.Field{{Name: "x", Type: "string"}},
		}})
		if !r.OK {
			t.Fatalf("add_entity %d (%s): %+v", i, name, r)
		}
	}

	// Wait for at least 5 tool rows to land via SSE.
	deadline := time.Now().Add(5 * time.Second) // 2s±10% poll cadence needs headroom
	for time.Now().Before(deadline) {
		var n int
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelectorAll('.kiln-msg-tool').length`, &n),
		); err == nil && n >= 5 {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	var shot []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
		t.Fatal(err)
	}
	saveShot(t, "05_rapid_edits_accumulated", shot)

	var n int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('.kiln-msg-tool').length`, &n),
	); err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Errorf("expected ≥5 system rows after 5 add_entity, got %d", n)
	}
}

// (5) THE CRITICAL ONE: user is sitting on a page, the agent updates
// it, a cache-bypass SPA navigation swaps in the new content without a
// document reload.
// This is the "live build" claim.
func TestVisual_LivePageHotReload(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	// Initial: page with heading "Version One"
	addV1 := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path:  "/draft",
		Title: "Draft",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Version One"}},
			{Kind: "paragraph", Children: []world.Node{
				{Kind: "text", Props: map[string]any{"value": "first cut"}},
			}},
		}},
	}})
	if !addV1.OK {
		t.Fatal(addV1)
	}

	// Navigate user to the page.
	var bodyV1 string
	var shotV1 []byte
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/draft"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.Text(`body`, &bodyV1, chromedp.ByQuery),
		chromedp.FullScreenshot(&shotV1, 90),
	); err != nil {
		t.Fatalf("navigate v1: %v", err)
	}
	saveShot(t, "06_live_v1_before", shotV1)
	if !strings.Contains(bodyV1, "Version One") {
		t.Fatalf("v1 body wrong: %q", bodyV1)
	}
	waitForPanelPoll(t, ctx)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__kilnDocumentSentinel = "same-document"`, nil),
	); err != nil {
		t.Fatalf("install document sentinel: %v", err)
	}

	// Agent rewrites the page in place: propose+approve a plan, delete, then add.
	if r := tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID:  "rewrite-draft",
		Steps:   []string{"replace /draft with v2"},
		Targets: []journal.PlanTarget{{Op: "delete_page", Name: "/draft"}},
	}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "rewrite-draft"}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.DeletePage(t.Context(), protocol.DeletePageArgs{Path: "/draft", PlanID: "rewrite-draft"}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path:  "/draft",
		Title: "Draft",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Version Two"}},
			{Kind: "paragraph", Children: []world.Node{
				{Kind: "text", Props: map[string]any{"value": "agent rewrote me hot"}},
			}},
		}},
	}}); !r.OK {
		t.Fatal(r)
	}

	// Browser should refresh through the GoFastr SPA runtime after the SSE edit.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var body string
		if err := chromedp.Run(ctx,
			chromedp.Text(`body`, &body, chromedp.ByQuery),
		); err == nil && strings.Contains(body, "Version Two") {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	var bodyV2 string
	var shotV2 []byte
	var sentinel string
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.Text(`body`, &bodyV2, chromedp.ByQuery),
		chromedp.Evaluate(`window.__kilnDocumentSentinel || ""`, &sentinel),
		chromedp.FullScreenshot(&shotV2, 90),
	); err != nil {
		t.Fatalf("post-refresh: %v", err)
	}
	saveShot(t, "07_live_v2_after_hot_reload", shotV2)

	if !strings.Contains(bodyV2, "Version Two") {
		t.Fatalf("HOT-RELOAD FAILED: still seeing v1 content after agent rewrote /draft. body=%q", bodyV2)
	}
	if !strings.Contains(bodyV2, "agent rewrote me hot") {
		t.Errorf("body missing new paragraph: %q", bodyV2)
	}
	if sentinel != "same-document" {
		t.Errorf("hot refresh replaced the browser document; want SPA navigation sentinel, got %q", sentinel)
	}
}

// (6) Verify the widget chrome itself rendered (translucent panel,
// FAB hidden when open, gradient background).
func TestVisual_PanelChromeStyling(t *testing.T) {
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	var panelOpen bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-widget`, chromedp.ByQuery),
		// Panel always opens immediately under the new framework — the
		// legacy FAB-toggle no longer exists. Just confirm the panel is
		// rendered with the expected open state.
		chromedp.Evaluate(`document.querySelector(".kiln-panel").classList.contains("kiln-open")`, &panelOpen),
	); err != nil {
		t.Fatal(err)
	}
	if !panelOpen {
		t.Error("panel should be open on host (always-open in new framework)")
	}

	var shot []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
		t.Fatal(err)
	}
	saveShot(t, "08_panel_styling", shot)
}

func waitForPanelPoll(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`!!(window.__gofastr && window.__gofastr.pollStatus && window.__gofastr.pollStatus.ticks > 0)`, &ready)); err == nil && ready {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatal("panel poll did not tick within 8s")
}

// Deleting a page the user is viewing must degrade to the Kiln host fallback,
// not a blank document: the SSE-triggered SPA refresh extracts <main> from the
// fallback response, so host.html must carry one.
func TestVisual_DeletePageShowsFallback(t *testing.T) {
	urlBase, tools := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	add := tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{
		Path:  "/doomed",
		Title: "Doomed",
		Tree: world.Node{Kind: "div", Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Short lived"}},
		}},
	}})
	if !add.OK {
		t.Fatal(add)
	}

	var body string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/doomed"),
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.Text(`body`, &body, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if !strings.Contains(body, "Short lived") {
		t.Fatalf("page body wrong before delete: %q", body)
	}
	waitForPanelPoll(t, ctx)

	if r := tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID:  "kill-doomed",
		Steps:   []string{"remove /doomed"},
		Targets: []journal.PlanTarget{{Op: "delete_page", Name: "/doomed"}},
	}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "kill-doomed"}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.DeletePage(t.Context(), protocol.DeletePageArgs{Path: "/doomed", PlanID: "kill-doomed"}); !r.OK {
		t.Fatal(r)
	}

	// The SSE edit forces an SPA refresh of /doomed, which now serves the
	// host fallback. The swapped-in content must not be blank.
	deadline := time.Now().Add(8 * time.Second)
	var after string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx,
			chromedp.Text(`body`, &after, chromedp.ByQuery),
		); err == nil && !strings.Contains(after, "Short lived") {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if strings.Contains(after, "Short lived") {
		t.Fatalf("refresh never happened; still showing deleted page: %q", after)
	}
	if !strings.Contains(after, "Talk to it") {
		t.Fatalf("EMPTY MAIN: host fallback content did not arrive after delete_page (host.html needs a <main> for the SPA swap): %q", after)
	}
}
