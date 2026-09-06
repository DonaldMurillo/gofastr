package main

// Browser end-to-end: one Chrome, two tabs, fake media, the real rtc
// runtime module and the real signaling battery. Ann and Bob join one
// room; camera flows peer to peer, chat rides the negotiated data
// channel, a mute lands as the other side's pill, and leaving removes
// the tile. Screenshots at the end are evidence for the reviewer, not
// assertions.
//
// Skips in -short mode. The tabs are driven serialized: one
// BringToFront before evaluating in a tab (a backgrounded tab's
// timers and media plumbing stall in headless Chrome), and form
// submits go through Click on the button (chromedp.Submit does not
// fire this runtime's submit path).

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework"

	"github.com/DonaldMurillo/gofastr/battery/rtc"
)

// callBrowserCtx boots a browser with the fake media flags (no real
// camera, no permission dialog) and tears it down with the test.
func callBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		// Without a getUserMedia grant, Chrome anonymizes ICE host
		// candidates as mDNS .local names; between two tabs of one
		// headless browser that resolution intermittently never
		// happens and ICE stays in 'new' forever. Emit real host
		// candidates (same flag the rtc runtime e2e suite passes).
		chromedp.Flag("disable-features", "WebRtcHideLocalIpsWithMdns"),
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
	ctx, tcancel := context.WithTimeout(tab, 300*time.Second)
	t.Cleanup(tcancel)
	return ctx
}

// run executes actions in a tab, foregrounding it first.
func run(t *testing.T, ctx context.Context, acts ...chromedp.Action) {
	t.Helper()
	all := append([]chromedp.Action{page.BringToFront()}, acts...)
	if err := chromedp.Run(ctx, all...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
}

// evalAwait evaluates an expression, awaiting any promise it returns.
func evalAwait(ctx context.Context, expr string, out *string) error {
	return chromedp.Run(ctx, chromedp.Evaluate(expr, out,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
}

func evalString(t *testing.T, ctx context.Context, expr string) string {
	t.Helper()
	var got string
	if err := evalAwait(ctx, expr, &got); err != nil {
		t.Fatalf("eval %s: %v", expr, err)
	}
	return got
}

// pollTrue polls a boolean expression until it is true. The
// expression is stringified in the page because Evaluate returns the
// raw JSON value, and a bool will not unmarshal into a string.
func pollTrue(t *testing.T, ctx context.Context, expr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	wrapped := "JSON.stringify(" + expr + ")"
	var got string
	for {
		if err := evalAwait(ctx, wrapped, &got); err != nil {
			t.Fatalf("poll: %v (expr %s)", err, expr)
		}
		if got == "true" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poll = %q, want true (expr %s, deadline)", got, expr)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// joinRoom drives the lobby form: name in, room in, submit, wait for
// the room page's config carrier.
func joinRoom(t *testing.T, ctx context.Context, base, name, room string) {
	t.Helper()
	run(t, ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`form[action="/join"] button[type=submit]`, chromedp.ByQuery),
		chromedp.SetValue("#call-name-input", name, chromedp.ByID),
		chromedp.SetValue("#call-room-input", room, chromedp.ByID),
		chromedp.Click(`form[action="/join"] button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible("#call-root", chromedp.ByID),
	)
}

// tileExpr builds an expression over the tile in this tab's grid whose
// name slot reads name.
func tileExpr(name, inner string) string {
	return fmt.Sprintf(`(() => {
		const tiles = document.querySelectorAll('#call-grid [data-call-tile]');
		for (const t of tiles) {
			const n = t.querySelector('[data-call-name]');
			if (n && n.textContent === %q) return (%s);
		}
		return false;
	})()`, name, inner)
}

// TestCallFlow drives Ann and Bob through one room: hydration, camera
// peer to peer, chat over the data channel, mute as a status document,
// and the leave that removes the tile.
func TestCallFlow(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	browser := callBrowserCtx(t)
	ann := newTab(t, browser) // tab A
	bob := newTab(t, browser) // tab B

	// ── 1. Both join room "e2e" through the lobby ────────────────
	joinRoom(t, ann, srv.URL, "Ann", "e2e")
	joinRoom(t, bob, srv.URL, "Bob", "e2e")
	// The notice is hidden for real on a fresh room: the attribute
	// alone lost to the component's display rule and every room opened
	// on an empty warning bar.
	if got := evalString(t, ann, `getComputedStyle(document.getElementById('call-notice')).display`); got != "none" {
		t.Fatalf("fresh-room #call-notice display = %q, want none", got)
	}

	pollTrue(t, ann, `window.__call.phase === 'hydrated'`, 15*time.Second)
	pollTrue(t, bob, `window.__call.phase === 'hydrated'`, 15*time.Second)

	// Each tab sees the other as a peer with a tile.
	pollTrue(t, ann, `window.__call.peers.length === 1`, 15*time.Second)
	pollTrue(t, bob, `window.__call.peers.length === 1`, 15*time.Second)

	// ── 2. Ann shares her camera; Bob receives it peer to peer ────
	run(t, ann, chromedp.Click("#call-share", chromedp.ByID))
	pollTrue(t, ann, `window.__call.media === 'sharing'`, 15*time.Second)
	// A denied share names the browser error class; that is the whole
	// diagnosis this surface can give.
	if m := evalString(t, ann, `window.__call.media`); m != "sharing" {
		t.Fatalf("ann share: %s", m)
	}
	pollTrue(t, bob,
		tileExpr("Ann", `!!t.querySelector('[data-call-video]').srcObject`), 30*time.Second)

	// ── 3. Bob shares too: his mute needs an audio track, and the
	// flow needs his camera live the way a real call would be.
	run(t, bob, chromedp.Click("#call-share", chromedp.ByID))
	pollTrue(t, bob, `window.__call.media === 'sharing'`, 15*time.Second)
	pollTrue(t, ann,
		tileExpr("Bob", `!!t.querySelector('[data-call-video]').srcObject`), 30*time.Second)

	// ── 4. Chat: Ann sends, Bob's list shows it with her name ─────
	run(t, ann,
		chromedp.SetValue("#call-chat-input", "hello", chromedp.ByID),
		chromedp.Click(`#call-chat-form button[type=submit]`, chromedp.ByQuery),
	)
	pollTrue(t, bob,
		`[...document.querySelectorAll('#call-chat li')].some(li => li.textContent.includes('Ann') && li.textContent.includes('hello'))`,
		15*time.Second)
	// The sender sees their own line too; a transcript missing your
	// own messages reads as a send that failed.
	pollTrue(t, ann,
		`[...document.querySelectorAll('#call-chat li')].some(li => li.textContent.includes('Ann') && li.textContent.includes('hello'))`,
		5*time.Second)

	// ── 5. Bob mutes; Ann's tile for Bob flips the muted pill ─────
	pollTrue(t, bob, `!document.getElementById('call-mute').disabled`, 15*time.Second)
	run(t, bob, chromedp.Click("#call-mute", chromedp.ByID))
	pollTrue(t, ann,
		tileExpr("Bob", `!t.querySelector('[data-call-pill="muted-on"]').hidden`), 15*time.Second)

	// The bounded debug surface never carried anything sensitive. Bob's
	// dump runs before his leave: leaving is a real navigation, and the
	// next document has no __call at all.
	for _, tab := range []struct {
		name string
		ctx  context.Context
	}{{"Ann", ann}, {"Bob", bob}} {
		dump := evalString(t, tab.ctx, `JSON.stringify(window.__call)`)
		for _, leak := range []string{"sdp", "candidate", "credential", "v=0"} {
			if strings.Contains(strings.ToLower(dump), leak) {
				t.Errorf("%s window.__call contains %q", tab.name, leak)
			}
		}
	}

	// ── 6. Bob leaves: a real navigation, and Ann's grid empties ──
	run(t, bob, chromedp.Click("#call-leave", chromedp.ByID))
	pollTrue(t, ann, `document.querySelectorAll('#call-grid [data-call-tile]').length === 0`, 20*time.Second)
	pollTrue(t, ann, `window.__call.peers.length === 0`, 15*time.Second)
	// A departed peer leaves the connection-state map too; a stale
	// "connected" for a peer who left is a lie the debug surface told.
	pollTrue(t, ann, `Object.keys(window.__call.states).length === 0`, 5*time.Second)

	// ── Evidence for the reviewer: both tabs, full page. ─────────
	saveShot(t, ann, "/tmp/rtc-call-A.png")
	saveShot(t, bob, "/tmp/rtc-call-B.png")
}

// saveShot captures a full-page screenshot to path.
func saveShot(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	var png []byte
	run(t, ctx, chromedp.FullScreenshot(&png, 90))
	if len(png) == 0 {
		t.Fatalf("empty screenshot %s", path)
	}
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("screenshot: %s", path)
}

// TestDeniedShareShowsNotice: a refused or absent camera is the first
// thing a locked-down laptop or a CI box hits. The page must say so
// where a person can read it, not only in the debug surface. The
// rejection is planted (the fake-device browser always grants); its
// shape is the browser's own DOMException.
func TestDeniedShareShowsNotice(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	browser := callBrowserCtx(t)
	cat := newTab(t, browser)
	joinRoom(t, cat, srv.URL, "Cat", "denied")
	pollTrue(t, cat, `window.__call.phase === 'hydrated'`, 15*time.Second)

	run(t, cat, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia = () => Promise.reject(new DOMException('denied', 'NotAllowedError'))`, nil))
	run(t, cat, chromedp.Click("#call-share", chromedp.ByID))
	pollTrue(t, cat, `(() => { const n = document.getElementById('call-notice'); return !!(n && !n.hidden && getComputedStyle(n).display !== 'none' && n.textContent.indexOf('NotAllowedError') >= 0); })()`, 10*time.Second)
	if got := evalString(t, cat, `String(document.getElementById('call-share').disabled)`); got != "false" {
		t.Fatalf("share button disabled = %s after a denial, want false (the user can retry)", got)
	}
}

// signalerOf reaches the rtc plugin the app registered, so a test can
// do what a load balancer or a flaky network does: drop one socket.
func signalerOf(t *testing.T, app *framework.App) *rtc.Signaler {
	t.Helper()
	for _, p := range app.Plugins.All() {
		if s, ok := p.(*rtc.Signaler); ok {
			return s
		}
	}
	t.Fatal("no rtc.Signaler plugin")
	return nil
}

// TestKickedSocketCallComesBack: the example sets no Join.PeerID, so a
// signaling reconnect lands under a new peer id. The module must
// rebuild its connections (every remote saw it leave), and video and
// chat come back. Before that rule a kicked tab kept its old
// connection against a remote that had rebuilt, and chat was dead
// for the rest of the call while the tiles looked fine.
func TestKickedSocketCallComesBack(t *testing.T) {
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	sig := signalerOf(t, app)
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	browser := callBrowserCtx(t)
	ann := newTab(t, browser)
	bob := newTab(t, browser)
	joinRoom(t, ann, srv.URL, "Ann", "kick")
	joinRoom(t, bob, srv.URL, "Bob", "kick")
	pollTrue(t, ann, `window.__call.peers.length === 1`, 20*time.Second)
	pollTrue(t, bob, `window.__call.peers.length === 1`, 20*time.Second)
	run(t, ann, chromedp.Click("#call-share", chromedp.ByID))
	pollTrue(t, ann, `window.__call.media === 'sharing'`, 20*time.Second)
	pollTrue(t, bob, tileExpr("Ann", `!!t.querySelector('[data-call-video]').srcObject`), 45*time.Second)

	var kicked bool
	for _, p := range sig.Peers("kick") {
		if p.DisplayName == "Bob" {
			kicked = sig.Kick("kick", p.ID)
		}
	}
	if !kicked {
		t.Fatal("Kick found no Bob")
	}
	pollTrue(t, bob, `window.__call.phase === 'hydrated' && window.__call.peers.length === 1`, 45*time.Second)
	pollTrue(t, bob, tileExpr("Ann", `!!t.querySelector('[data-call-video]').srcObject`), 60*time.Second)

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		run(t, bob,
			chromedp.SetValue("#call-chat-input", "after-kick", chromedp.ByID),
			chromedp.Click(`#call-chat-form button[type=submit]`, chromedp.ByQuery),
		)
		time.Sleep(2 * time.Second)
		if evalString(t, ann, `String([...document.querySelectorAll('#call-chat li')].some(li => li.textContent.includes('after-kick')))`) == "true" {
			return
		}
	}
	t.Fatal("chat never reached Ann after Bob's socket was kicked")
}
