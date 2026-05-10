package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	_ "github.com/mattn/go-sqlite3"

	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/protocol"
	"github.com/gofastr/gofastr/kiln/world"
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

// (1) Empty state on host renders the welcome cards + the floating
// expanded panel with empty message.
func TestVisual_HostEmptyState(t *testing.T) {
	t.Skip("host empty-state DOM (.kiln-empty) was a legacy widget.js construct; new panel mounts immediately — needs visual test rewrite")
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	var shot []byte
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-empty`, chromedp.ByQuery),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		chromedp.FullScreenshot(&shot, 90),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	path := saveShot(t, "01_host_empty", shot)
	t.Logf("saved: %s", path)
}

// (2) Status text is visible mid-flight. We slow the network so the
// pending state stays on screen long enough to capture.
func TestVisual_StatusFeedbackOnSend(t *testing.T) {
	t.Skip(".kiln-status was a legacy widget.js DOM node; status feedback in the new panel surfaces via signals — needs visual test rewrite")
	urlBase, _ := startKiln(t)
	ctx, cancel := newChrome(t)
	defer cancel()

	var statusVisibleText string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(urlBase+"/"),
		chromedp.WaitVisible(`.kiln-input`, chromedp.ByQuery),
		// Stub fetch with a 400ms delay so the "sending…" / "sent" text
		// is observable to a human eye AND to chromedp.
		chromedp.Evaluate(`(function(){
			const orig = window.fetch;
			window.fetch = (u, o) => new Promise((res) => {
				setTimeout(() => res(orig(u, o)), 400);
			});
		})()`, nil),
		chromedp.SendKeys(`.kiln-input`, "watch the status"),
		chromedp.Click(`.kiln-send`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Capture the pending status while the request is in-flight.
	var shotPending []byte
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`.kiln-status`, chromedp.ByQuery),
		chromedp.Text(`.kiln-status`, &statusVisibleText, chromedp.ByQuery),
		chromedp.FullScreenshot(&shotPending, 90),
	); err != nil {
		t.Fatalf("pending: %v", err)
	}
	saveShot(t, "02_status_pending", shotPending)

	if !strings.Contains(statusVisibleText, "sending") &&
		!strings.Contains(statusVisibleText, "ok") &&
		!strings.Contains(statusVisibleText, "sent") {
		t.Errorf("expected sending/ok/sent in status, got %q", statusVisibleText)
	}

	// Capture the success status before it auto-clears.
	if err := chromedp.Run(ctx,
		chromedp.Sleep(600*time.Millisecond), // let the post resolve
	); err != nil {
		t.Fatal(err)
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
	waitForSSE(t, ctx)

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
	waitForSSE(t, ctx)

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
	deadline := time.Now().Add(5 * time.Second)
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
// it, browser reloads automatically and the new content is visible.
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
	waitForSSE(t, ctx)

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

	// Browser should auto-reload via SSE → location.reload().
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
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.Text(`body`, &bodyV2, chromedp.ByQuery),
		chromedp.FullScreenshot(&shotV2, 90),
	); err != nil {
		t.Fatalf("post-reload: %v", err)
	}
	saveShot(t, "07_live_v2_after_hot_reload", shotV2)

	if !strings.Contains(bodyV2, "Version Two") {
		t.Fatalf("HOT-RELOAD FAILED: still seeing v1 content after agent rewrote /draft. body=%q", bodyV2)
	}
	if !strings.Contains(bodyV2, "agent rewrote me hot") {
		t.Errorf("body missing new paragraph: %q", bodyV2)
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

// waitForSSE polls until the EventSource connects.
func waitForSSE(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__fuiSSEReady`, &ready)); err == nil && ready {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatal("SSE did not open within 8s")
}
