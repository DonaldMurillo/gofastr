package headless

// The two toggle buttons follow the APG toggle-button pattern: one
// accessible name that never changes, and aria-pressed carrying the
// state. Read from the browser's own accessibility tree, the name a
// screen reader announces, not from an attribute the name might ignore.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// axNameOf returns the computed accessible name of the first element
// matching sel.
func axNameOf(t *testing.T, ctx context.Context, sel string) string {
	t.Helper()
	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(sel, &nodes, chromedp.ByQuery)); err != nil {
		t.Fatalf("finding %s: %v", sel, err)
	}
	var name string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		ax, err := accessibility.GetPartialAXTree().WithBackendNodeID(nodes[0].BackendNodeID).WithFetchRelatives(false).Do(ctx)
		if err != nil {
			return err
		}
		if len(ax) == 0 || ax[0].Name == nil {
			return nil
		}
		return json.Unmarshal(ax[0].Name.Value, &name)
	})); err != nil {
		t.Fatalf("reading the accessible name of %s: %v", sel, err)
	}
	return name
}

func TestE2E_ToggleButtonsKeepOneAccessibleName(t *testing.T) {
	toggle := ToggleAction(ToggleActionProps{
		Endpoint: "/__hui/ok", IdleLabel: "Watch", CommittedLabel: "Watching", AllowUntoggle: true,
	}, nil)
	pw := Password(PasswordProps{Name: "pw", ID: "pw"}, nil)
	b := startBehaviorServer(t, string(toggle)+string(pw))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}

	const btn = `[data-hui-action]`
	if got := axNameOf(t, ctx, btn); got != "Watch" {
		t.Fatalf("idle toggle is named %q, want Watch", got)
	}
	if err := chromedp.Run(ctx, chromedp.Click(btn, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-action]').getAttribute('data-state') === 'committed'`) {
		t.Fatal("the toggle never committed")
	}
	if got := evalString(t, ctx, `document.querySelector('[data-hui-action]').getAttribute('aria-pressed')`); got != "true" {
		t.Fatalf("committed toggle has aria-pressed=%q, want true", got)
	}
	if got := axNameOf(t, ctx, btn); got != "Watch" {
		t.Fatalf("committed toggle is named %q, want Watch: the name stays and aria-pressed says it is on", got)
	}
	if got := evalString(t, ctx, `String(document.querySelector('[data-hui-action-done]').hidden)`); got != "false" {
		t.Fatal("the committed label is not shown: the visible label still flips")
	}
	if err := chromedp.Run(ctx, chromedp.Click(btn, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-action]').getAttribute('data-state') === 'idle'`) {
		t.Fatal("the toggle never untoggled")
	}
	if got := axNameOf(t, ctx, btn); got != "Watch" {
		t.Fatalf("untoggled toggle is named %q, want Watch", got)
	}

	const reveal = `[data-hui-reveal]`
	if err := chromedp.Run(ctx, chromedp.Click(reveal, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.querySelector('[data-hui-reveal]').getAttribute('aria-pressed')`); got != "true" {
		t.Fatalf("revealed password's button has aria-pressed=%q, want true", got)
	}
	if got := axNameOf(t, ctx, reveal); got != "Show password" {
		t.Fatalf("pressed reveal button is named %q, want Show password", got)
	}
}
