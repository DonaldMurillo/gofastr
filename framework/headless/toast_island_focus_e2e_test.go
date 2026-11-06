package headless

import (
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestIslandToastReturnsFocus(t *testing.T) {
	for _, test := range []struct {
		name      string
		ttl       int
		moveFocus bool
		refuse    bool
	}{
		{name: "persistent"},
		{name: "timed", ttl: 60000},
		{name: "focus moved", moveFocus: true},
		{name: "refused dismissal", refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requested, release := make(chan struct{}, 1), make(chan struct{}, 1)
			toast := Toast(ToastProps{ID: "server-toast", Title: "Saved", TTLMS: test.ttl,
				DismissHref: "/dismiss", Island: Island{Endpoint: "/island/dismiss", Signal: "notices"}}, nil)
			region := render.HTML(`<div data-cui-signal="notices" data-cui-signal-mode="html">` + string(toast) + `</div>`)
			body := `<button id="origin">Origin</button><button id="other">Other</button>` +
				string(ToastStack(ToastStackProps{Label: "News", Toasts: []render.HTML{region}}, nil))
			server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
				mux.HandleFunc("/island/dismiss", func(writer http.ResponseWriter, request *http.Request) {
					requested <- struct{}{}
					<-release
					writer.Header().Set("Content-Type", "text/html")
					if test.refuse {
						http.Error(writer, "dismissal refused", http.StatusConflict)
						return
					}
					writer.WriteHeader(http.StatusOK)
				})
			})
			t.Cleanup(func() { close(release) })
			ctx := behaviorPage(t, server)
			if err := chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
				t.Fatal("feedback did not load")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.getElementById('origin').focus(); document.querySelector('#server-toast [data-hui-toast-dismiss]').focus()`, nil),
				chromedp.KeyEvent("\r"),
			); err != nil {
				t.Fatal(err)
			}
			select {
			case <-requested:
			case <-time.After(5 * time.Second):
				t.Fatal("dismiss did not reach its Island endpoint")
			}
			want := "origin"
			if test.moveFocus {
				want = "other"
				if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('other').focus()`, nil)); err != nil {
					t.Fatal(err)
				}
			}
			release <- struct{}{}
			if test.refuse {
				if !pollTrue(ctx, `window.__gofastr.getSignal('notices')?.ok === false`) {
					t.Fatal("refused dismissal did not reach its signal")
				}
				if !pollTrue(ctx, `document.activeElement === document.querySelector('#server-toast [data-hui-toast-dismiss]')`) {
					t.Fatal("refused dismissal moved focus or removed its row")
				}
				return
			}
			if !pollTrue(ctx, `!document.getElementById('server-toast')`) {
				t.Fatal("Island response did not remove the toast")
			}
			if !pollTrue(ctx, `document.activeElement.id === '`+want+`'`) {
				t.Fatalf("Island dismissal focus = %s, want %s", evalString(t, ctx, `document.activeElement.tagName + '#' + document.activeElement.id`), want)
			}
		})
	}
}
