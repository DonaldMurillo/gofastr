package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// dialogChrome is the markup core-ui/widget's defaultSkeleton emits for a
// centered modal (TestModalChromeIsADialog pins the server side).
func dialogChrome(name, attrs string) string {
	return `<dialog class="cui-widget cui-pos-center" data-cui-widget="` + name + `" aria-label="` + name + `" open` + attrs + `>` +
		`<div class="cui-panel"><div class="cui-slot cui-slot-body"><button id="` + name + `-a">a</button><button id="` + name + `-b">b</button></div></div></dialog>`
}

// dialogServer serves the runtime, a page with a trigger and an outside
// button, and chrome from the map. widgetfocus.js is held until free.
func dialogServer(t *testing.T, widgets string, chrome map[string]string) (url string, free func()) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("widgetfocus")
	if !ok {
		t.Fatal("widgetfocus module not embedded")
	}
	release := make(chan struct{})
	var once sync.Once
	free = func() { once.Do(func() { close(release) }) }
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime/widgetfocus.js", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, mod)
	})
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, widgets)
	})
	mux.HandleFunc("/chrome/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, chrome[strings.TrimPrefix(r.URL.Path, "/chrome/")])
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>dlg</title></head><body><main>
<button id="trigger">open</button><button id="outside">outside</button></main>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(free)
	return srv.URL, free
}

func dialogCfg(name string, escape, click bool) string {
	return fmt.Sprintf(`{"hidden":true,"cfg":{"name":%q,"chromePath":"/chrome/%s","backdrop":true,"closeOnEscape":%t,"closeOnClick":%t}}`, name, name, escape, click)
}

func openDialog(name string) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Focus(`#trigger`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.openWidget('`+name+`')`, nil, chromedp.EvalAsValue),
		chromedp.Poll(`document.activeElement?.id === '`+name+`-a'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	}
}

// A default-chrome modal opens as a modal <dialog>: in the top layer, no
// backdrop div, and the page scroll locked.
func TestModalWidgetOpensAsNativeDialog(t *testing.T) {
	url, _ := dialogServer(t, "["+dialogCfg("dlg", true, true)+"]", map[string]string{"dlg": dialogChrome("dlg", "")})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got struct {
		Modal, Backdrop, Locked bool
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		openDialog("dlg"),
		chromedp.Evaluate(`({
			Modal: document.querySelector('[data-cui-widget="dlg"]').matches(':modal'),
			Backdrop: !!document.querySelector('[data-cui-backdrop]'),
			Locked: document.documentElement.style.overflow === 'hidden',
		})`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !got.Modal {
		t.Error("the modal widget's <dialog> is not :modal; showModal() was not called")
	}
	if got.Backdrop {
		t.Error("a backdrop div was created for a <dialog> root; ::backdrop paints the scrim")
	}
	if !got.Locked {
		t.Error("page scroll is not locked while the dialog is open")
	}
}

// #753: Tab pressed before widgetfocus loads never moves focus out of the
// modal, because the browser makes the page inert from the moment it opens.
func TestTabBeforeFocusModuleStaysInDialog(t *testing.T) {
	url, _ := dialogServer(t, "["+dialogCfg("dlg", true, true)+"]", map[string]string{"dlg": dialogChrome("dlg", "")})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var escaped []string
	tabs := chromedp.Tasks{}
	for range 5 {
		tabs = append(tabs, chromedp.KeyEvent(kb.Tab), chromedp.ActionFunc(func(ctx context.Context) error {
			var id string
			if err := chromedp.Evaluate(`(() => { const a = document.activeElement; return a && a.closest('main') ? a.id : ''; })()`, &id).Do(ctx); err != nil {
				return err
			}
			if id != "" {
				escaped = append(escaped, id)
			}
			return nil
		}))
	}
	var loaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		openDialog("dlg"),
		tabs,
		chromedp.Evaluate(`!!window.__gofastr.loadedModules.widgetfocus`, &loaded),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if loaded {
		t.Fatal("setup: widgetfocus loaded; the held response did not hold")
	}
	if len(escaped) > 0 {
		t.Fatalf("Tab moved focus out of the open modal to %v", escaped)
	}
}

// Escape, with widgetfocus never loaded, closes the dialog and hands focus
// back to the trigger; a modal that refuses Escape stays open.
func TestDialogEscapeClosesWithoutFocusModule(t *testing.T) {
	url, _ := dialogServer(t, "["+dialogCfg("dlg", true, true)+","+dialogCfg("stay", false, false)+"]", map[string]string{
		"dlg":  dialogChrome("dlg", ""),
		"stay": dialogChrome("stay", ` closedby="none"`),
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var closed, back, stayOpen, unlocked bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		openDialog("dlg"),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!document.querySelector('[data-cui-widget="dlg"]')`, &closed, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement?.id === 'trigger'`, &back),
		chromedp.Evaluate(`document.documentElement.style.overflow === ''`, &unlocked),
		openDialog("stay"),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="stay"]')?.matches(':modal')`, &stayOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !closed {
		t.Fatal("Escape did not close the <dialog> modal")
	}
	if !back {
		t.Error("focus did not return to the trigger after Escape")
	}
	if !unlocked {
		t.Error("page scroll stayed locked after the dialog closed")
	}
	if !stayOpen {
		t.Error("Escape closed a modal declared closeOnEscape=false")
	}
}

// A click on the scrim closes the dialog; a press that starts in the panel
// and ends on the scrim (a dragged text selection) does not.
func TestDialogBackdropClickCloses(t *testing.T) {
	url, _ := dialogServer(t, "["+dialogCfg("dlg", true, true)+"]", map[string]string{"dlg": dialogChrome("dlg", "")})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var afterDrag, afterClick bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(800, 600),
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		openDialog("dlg"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var r struct{ X, Y float64 }
			if err := chromedp.Evaluate(`(() => { const b = document.getElementById('dlg-a').getBoundingClientRect(); return {X: b.x + 2, Y: b.y + 2}; })()`, &r).Do(ctx); err != nil {
				return err
			}
			return chromedp.Tasks{
				chromedp.MouseEvent("mousePressed", r.X, r.Y, chromedp.ButtonLeft, chromedp.ClickCount(1)),
				chromedp.MouseEvent("mouseMoved", 5, 5, chromedp.ButtonLeft),
				chromedp.MouseEvent("mouseReleased", 5, 5, chromedp.ButtonLeft, chromedp.ClickCount(1)),
			}.Do(ctx)
		}),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="dlg"]')`, &afterDrag),
		chromedp.MouseClickXY(5, 5),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="dlg"]')`, &afterClick),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !afterDrag {
		t.Error("a press started in the panel and released on the scrim closed the dialog")
	}
	if afterClick {
		t.Error("a click on the scrim did not close the dialog")
	}
}

// A panel opened over a <dialog> modal mounts inside it, so it is not inert
// under the top layer, and goes back to <body> when the modal closes.
func TestPanelOverDialogMountsInside(t *testing.T) {
	panel := `{"hidden":true,"cfg":{"name":"pop","chromePath":"/chrome/pop","closeOnEscape":true}}`
	url, free := dialogServer(t, "["+dialogCfg("dlg", true, true)+","+panel+"]", map[string]string{
		"dlg": dialogChrome("dlg", ""),
		"pop": `<div class="cui-widget cui-pos-top-right" data-cui-widget="pop"><button id="pop-btn">pop</button></div>`,
	})
	free()
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var inside, clickable, popEsc, dlgOpen, outBody bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		openDialog("dlg"),
		chromedp.Evaluate(`window.__gofastr.openWidget('pop')`, nil, chromedp.EvalAsValue),
		chromedp.WaitVisible(`#pop-btn`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('[data-cui-widget="dlg"]').contains(document.getElementById('pop-btn'))`, &inside),
		chromedp.Evaluate(`(() => { const b = document.getElementById('pop-btn').getBoundingClientRect(); return document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2)?.id === 'pop-btn'; })()`, &clickable),
		// Escape closes the panel above the modal, and only the panel.
		chromedp.Poll(`!!window.__gofastr.loadedModules.widgetfocus`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Focus(`#pop-btn`, chromedp.ByID),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-cui-widget="pop"]')`, &popEsc),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="dlg"]')?.matches(':modal')`, &dlgOpen),
		// Reopen the panel, close the modal under it: the panel lands in <body>.
		chromedp.Evaluate(`window.__gofastr.openWidget('pop')`, nil, chromedp.EvalAsValue),
		chromedp.WaitVisible(`#pop-btn`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.closeWidget('dlg')`, nil),
		chromedp.Evaluate(`document.querySelector('[data-cui-widget="pop"]')?.parentElement === document.body`, &outBody),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !inside {
		t.Error("the panel opened over the dialog was not mounted inside it")
	}
	if !clickable {
		t.Error("the panel opened over the dialog does not take the pointer")
	}
	if !popEsc {
		t.Error("Escape did not close the panel above the modal")
	}
	if !dlgOpen {
		t.Error("the Escape that closed the panel above the modal also closed the modal")
	}
	if !outBody {
		t.Error("the panel did not move back to <body> when the dialog under it closed")
	}
}
