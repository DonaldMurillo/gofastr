package headless

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestComboboxMiddleClickOpensTab(t *testing.T) {
	server := startBehaviorServer(t, string(Combobox(ComboboxProps{
		ID: "links", Name: "q", Label: "Search", Options: []ComboboxOption{
			{Label: "Birch", Href: "/birch?view=tree#leaves", Meta: "Tree"},
		},
	}, nil)))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	var point struct{ X, Y float64 }
	if err := chromedp.Run(ctx,
		chromedp.Click(`#links`, chromedp.ByID),
		chromedp.Evaluate(`window.picks = 0; window.__gofastr.navigate = () => window.picks++;`, nil),
		chromedp.Evaluate(`(() => { const box = document.querySelector('#links-listbox [role="option"] span').getBoundingClientRect(); return {X: box.x + box.width / 2, Y: box.y + box.height / 2}; })()`, &point),
	); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("GOFASTR_SCREENSHOT_DIR"); directory != "" {
		var image []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&image, 90)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "combobox-links.png"), image, 0600); err != nil {
			t.Fatal(err)
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opened := make(chan target.ID, 1)
	chromedp.ListenBrowser(waitCtx, func(event any) {
		var info *target.Info
		switch event := event.(type) {
		case *target.EventTargetCreated:
			info = event.TargetInfo
		case *target.EventTargetInfoChanged:
			info = event.TargetInfo
		}
		if info != nil && info.URL == server.srv.URL+"/birch?view=tree#leaves" {
			select {
			case opened <- info.TargetID:
			default:
			}
		}
	})
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchMouseEvent(input.MousePressed, point.X, point.Y).WithButton(input.Middle).WithClickCount(1).Do(ctx); err != nil {
			return err
		}
		return input.DispatchMouseEvent(input.MouseReleased, point.X, point.Y).WithButton(input.Middle).WithClickCount(1).Do(ctx)
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case targetID := <-opened:
		if targetID == "" {
			t.Fatal("middle-click did not open the option destination")
		}
		defer chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return target.CloseTarget(targetID).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		}))
	case <-waitCtx.Done():
		t.Fatal("middle-click did not open the option destination")
	}
	var untouched bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.picks === 0 && document.getElementById('links').value === '' && document.activeElement.id === 'links' && document.querySelector('#links-listbox a[role="option"]').tabIndex === -1`, &untouched)); err != nil {
		t.Fatal(err)
	}
	if !untouched {
		t.Fatal("native navigation picked a value or moved combobox focus")
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(kb.Enter), chromedp.Poll(`window.picks === 1 && document.getElementById('links').value === 'Birch'`, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestComboboxKeepsModifiedLinks(t *testing.T) {
	server := startBehaviorServer(t, string(Combobox(ComboboxProps{
		ID: "links", Name: "q", Label: "Search", Options: []ComboboxOption{{Label: "Cedar", Href: "/cedar"}},
	}, nil)))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.picks = 0; window.__gofastr.navigate = () => window.picks++;`, nil)); err != nil {
		t.Fatal(err)
	}
	for _, modifiers := range []string{`metaKey: true`, `ctrlKey: true`, `shiftKey: true`, `altKey: true`, `button: 1`, `button: 2`} {
		var preserved bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const event = new MouseEvent('click', {bubbles: true, cancelable: true, `+modifiers+`});
			let prevented;
			window.addEventListener('click', e => { prevented = e.defaultPrevented; e.preventDefault(); }, {once: true});
			document.querySelector('#links-listbox [role="option"] span').dispatchEvent(event);
			return !prevented && window.picks === 0 && document.getElementById('links').value === '';
		})()`, &preserved)); err != nil {
			t.Fatal(err)
		}
		if !preserved {
			t.Errorf("combobox consumed a modified link (%s)", modifiers)
		}
	}
	var picked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const event = new MouseEvent('click', {bubbles: true, cancelable: true});
		document.querySelector('#links-listbox [role="option"]').dispatchEvent(event);
		return event.defaultPrevented && window.picks === 1 && document.getElementById('links').value === 'Cedar';
	})()`, &picked)); err != nil {
		t.Fatal(err)
	}
	if !picked {
		t.Error("ordinary primary click no longer picks the option")
	}
}
