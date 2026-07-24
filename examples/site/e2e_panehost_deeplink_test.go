package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The contract PaneHost deep-linking promises is that a URL carrying
// ?pane= renders the pane open FROM THE SERVER. Asserting that in a
// browser would not distinguish "server rendered it open" from "the
// runtime opened it after hydration", which is the whole difference —
// the second is a flash of closed pane on every shared link. So this
// half runs over plain HTTP with no JavaScript at all.
func TestWorkspaceDeepLinkRendersServerSide(t *testing.T) {
	base := siteE2EServer(t)

	get := func(t *testing.T, path string) string {
		t.Helper()
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(body)
	}

	t.Run("open and filled", func(t *testing.T) {
		html := get(t, "/examples/workspace?pane=secondary:4021")
		if !strings.Contains(html, "ui-pane-host--secondary-open") {
			t.Error("deep-linked pane did not render open")
		}
		// Content, not just the column: the ticket body has to be in the
		// first response or the link still flashes empty.
		if !strings.Contains(html, "SSO login") {
			t.Error("deep-linked pane rendered open but empty")
		}
		if !strings.Contains(html, `data-fui-pane-deeplink="pane"`) {
			t.Error("host is missing the deep-link marker the runtime keys off")
		}
	})

	t.Run("no param renders closed", func(t *testing.T) {
		html := get(t, "/examples/workspace")
		if strings.Contains(html, "ui-pane-host--secondary-open") {
			t.Error("pane rendered open without a deep link")
		}
		if !strings.Contains(html, "Select a ticket") {
			t.Error("expected the empty state")
		}
	})

	// An edited or stale URL must degrade to the ordinary page, never
	// error and never echo the key back into the document.
	t.Run("unknown ticket degrades", func(t *testing.T) {
		html := get(t, "/examples/workspace?pane=secondary:not-a-ticket")
		if strings.Contains(html, "ui-pane-host--secondary-open") {
			t.Error("unknown key should not open the pane")
		}
		if strings.Contains(html, "not-a-ticket") {
			t.Error("unresolved key was reflected into the page")
		}
	})

	t.Run("bogus slot degrades", func(t *testing.T) {
		html := get(t, "/examples/workspace?pane=primary:4021")
		if strings.Contains(html, "ui-pane-host--secondary-open") {
			t.Error("primary is not an openable pane and must not deep-link")
		}
	})
}

// The browser half: opening writes the URL, Back closes, Forward
// restores both the pane AND its content (the replayed trigger re-runs
// the RPC). The tertiary pane is deliberately unkeyed, so opening it
// must leave the URL alone.
func TestWorkspaceDeepLinkRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	const row = `document.querySelector('button[data-fui-pane-key="4021"]')`
	const paneParam = `new URL(location.href).searchParams.get('pane')`
	const secOpen = `document.querySelector('[data-fui-pane="secondary"]').hasAttribute('hidden') === false`

	var afterOpen, afterBack, afterFwd string
	var openState, backState, fwdState bool
	var detailAfterFwd, paramAfterTertiary string

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/workspace"),
		chromedp.WaitVisible(`button[data-fui-pane-key="4021"]`),

		// Open: pane shows and the URL records which one.
		chromedp.Evaluate(row+`.click()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(paneParam, &afterOpen),
		chromedp.Evaluate(secOpen, &openState),

		// The customer pane carries no key, so it must not disturb the
		// ticket's deep link.
		chromedp.Evaluate(`document.querySelector('[data-fui-pane-open="tertiary"]').click()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(paneParam, &paramAfterTertiary),

		// Back closes.
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(paneParam, &afterBack),
		chromedp.Evaluate(secOpen, &backState),

		// Forward reopens — and refills, which is the part a naive
		// implementation gets wrong (column back, content blank).
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(paneParam, &afterFwd),
		chromedp.Evaluate(secOpen, &fwdState),
		chromedp.Evaluate(`document.querySelector('[data-fui-signal="ws-ticket"]').textContent`, &detailAfterFwd),
	); err != nil {
		t.Fatal(err)
	}

	if afterOpen != "secondary:4021" {
		t.Errorf("open did not record the pane: pane=%q", afterOpen)
	}
	if !openState {
		t.Error("secondary pane is not open after the row click")
	}
	if paramAfterTertiary != "secondary:4021" {
		t.Errorf("unkeyed tertiary trigger changed the URL: pane=%q", paramAfterTertiary)
	}
	if afterBack != "" {
		t.Errorf("Back left the pane in the URL: pane=%q", afterBack)
	}
	if backState {
		t.Error("Back did not close the pane")
	}
	if afterFwd != "secondary:4021" {
		t.Errorf("Forward did not restore the URL: pane=%q", afterFwd)
	}
	if !fwdState {
		t.Error("Forward did not reopen the pane")
	}
	if !strings.Contains(detailAfterFwd, "SSO login") {
		t.Errorf("Forward reopened an empty pane — content was not replayed: %q", detailAfterFwd)
	}
}
