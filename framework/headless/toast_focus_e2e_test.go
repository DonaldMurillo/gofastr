package headless

import (
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestToastDismissRestoresFocus(t *testing.T) {
	for _, control := range []string{"dismiss", "action"} {
		for _, testCase := range []struct {
			name     string
			neighbor bool
			tabIndex string
		}{{"external", false, "0"}, {"next", true, "0"}, {"programmatic", false, "-1"}} {
			t.Run(control+"/"+testCase.name, func(t *testing.T) {
				ctx := behaviorPage(t, startBehaviorServer(t, `<button id="origin" tabindex="`+testCase.tabIndex+`">Origin</button>`+toastStackWithTemplate(), func(mux *http.ServeMux) {
					mux.HandleFunc("/undo", func(w http.ResponseWriter, request *http.Request) {
						w.WriteHeader(http.StatusNoContent)
					})
				}))
				if err := chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true)); err != nil {
					t.Fatal(err)
				}
				if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
					t.Fatal("feedback did not load")
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('origin').focus();
					window.__firstToast = window.__gofastr.toast({title:'First', ttl:60000, action:{label:'Undo', attrs:{'data-cui-rpc':'/undo'}}});
					window.__firstRow = document.querySelector('[data-hui-toast-id="' + window.__firstToast + '"]');
					window.__firstTimer = window.__gofastr._toastTimers.get(window.__firstToast);`, nil)); err != nil {
					t.Fatal(err)
				}
				if testCase.neighbor {
					if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__nextToast = window.__gofastr.toast({title:'Next'});`, nil)); err != nil {
						t.Fatal(err)
					}
				}
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`window.__firstRow.querySelector('[data-hui-toast-`+control+`]').focus()`, nil),
					chromedp.KeyEvent("\r"),
				); err != nil {
					t.Fatal(err)
				}
				if !pollTrue(ctx, `!window.__firstRow.isConnected`) {
					t.Fatal("toast did not leave after keyboard activation")
				}
				expected := `document.activeElement === document.getElementById('origin')`
				if testCase.neighbor {
					expected = `document.activeElement === document.querySelector('[data-hui-toast-id="' + window.__nextToast + '"] [data-hui-toast-dismiss]')`
				}
				if !pollTrue(ctx, expected) {
					t.Fatalf("toast dismissal dropped focus instead of returning it: %s", evalString(t, ctx, `document.activeElement.tagName`))
				}
				if !pollTrue(ctx, `window.__firstTimer.timer === 0 && !window.__gofastr._toastTimers.has(window.__firstToast)`) {
					t.Fatal("returning focus rearmed a leaving toast's timer")
				}
			})
		}
	}
}

func TestToastFocusSkipsUnavailableRows(t *testing.T) {
	ctx := behaviorPage(t, startBehaviorServer(t, toastStackWithTemplate()))
	if err := chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("feedback did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__previous = window.__gofastr.toast({title:'Previous'});
		const current = window.__gofastr.toast({title:'Current'});
		const next = window.__gofastr.toast({title:'Unavailable'});
		document.querySelector('[data-hui-toast-id="' + next + '"] button').disabled = true;
		const control = document.querySelector('[data-hui-toast-id="' + current + '"] [data-hui-toast-dismiss]');
		control.focus(); control.click();
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.querySelector('[data-hui-toast-id="' + window.__previous + '"] [data-hui-toast-dismiss]')`) {
		t.Fatal("dismissal did not skip unavailable next controls for the previous row")
	}
}

func TestToastRemovalRespectsNewFocus(t *testing.T) {
	ctx := behaviorPage(t, startBehaviorServer(t, `<button id="origin">Origin</button><button id="other">Other</button>`+toastStackWithTemplate()))
	if err := chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("feedback did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { document.getElementById('origin').focus();
		const id = window.__gofastr.toast({title:'Leaving'});
		window.__row = document.querySelector('[data-hui-toast-id="' + id + '"]');
		const dismiss = window.__row.querySelector('[data-hui-toast-dismiss]');
		dismiss.focus(); dismiss.click(); document.getElementById('other').focus(); })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!window.__row.isConnected && document.activeElement.id === 'other'`) {
		t.Fatal("dismissal stole focus moved outside during the exit")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const id = window.__gofastr.toast({title:'Timed', ttl:50});
		window.__timed = document.querySelector('[data-hui-toast-id="' + id + '"]'); })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!window.__timed.isConnected && document.activeElement.id === 'other'`) {
		t.Fatal("automatic removal stole external focus")
	}
}

func TestToastCapacityPreservesFocus(t *testing.T) {
	ctx := behaviorPage(t, startBehaviorServer(t, toastStackWithTemplate()))
	if err := chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("feedback did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-cui-toast-stack]').setAttribute('data-hui-toast-max', '1');
		const id = window.__gofastr.toast({title:'First'});
		document.querySelector('[data-hui-toast-id="' + id + '"] [data-hui-toast-dismiss]').focus();
		window.__latest = window.__gofastr.toast({title:'Second'});`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.querySelector('[data-hui-toast-id="' + window.__latest + '"] [data-hui-toast-dismiss]')`) {
		t.Fatal("capacity eviction dropped focus from its focused row")
	}
}
