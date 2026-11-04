package runtime

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestHelpersLoadWithoutWidgets(t *testing.T) {
	cases := map[string]string{
		"fill":             `<input id="target"><button data-cui-fill-input="#target">Fill</button>`,
		"elapsed":          `<span data-cui-tick-elapsed="1"></span>`,
		"persist":          `<textarea data-cui-persist-storage="probe"></textarea>`,
		"count":            `<input id="source"><span data-cui-charcount-source="#source"></span>`,
		"clear":            `<textarea data-cui-clear-on-esc></textarea>`,
		"enter":            `<form data-cui-submit-on-enter><textarea></textarea></form>`,
		"invalid":          `<form data-cui-disable-when-invalid><input required><button>Save</button></form>`,
		"invalid-button":   `<form><input required><button data-cui-disable-when-invalid>Save</button><button type="submit">Other</button></form>`,
		"invalid-external": `<form id="save"><input required></form><button form="save" data-cui-disable-when-invalid>Save</button>`,
	}
	for name, markup := range cases {
		for _, inserted := range []bool{false, true} {
			mode := "boot"
			body := markup
			if inserted {
				mode, body = "insert", ""
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				if !slices.Contains(NeededModules(markup), "widgethelpers") {
					t.Error("server preloads omit the helper module")
				}
				url := widgetHelpersServer(t, body)
				ctx := chromedptest.Context(t)
				if err := chromedp.Run(ctx,
					chromedp.Navigate(url),
					chromedp.Poll(`typeof window.__gofastr?.loadModule === 'function'`, nil),
				); err != nil {
					t.Fatal(err)
				}
				if inserted {
					var loaded bool
					encoded, _ := json.Marshal(markup)
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(`!!window.__gofastr.loadedModules?.widgethelpers`, &loaded),
						chromedp.Evaluate(`document.querySelector('main').insertAdjacentHTML('beforeend', `+string(encoded)+`)`, nil),
					); err != nil {
						t.Fatal(err)
					}
					if loaded {
						t.Fatal("helpers loaded without any helper marker")
					}
				}
				if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr.loadedModules?.widgethelpers`, nil,
					chromedp.WithPollingTimeout(2*time.Second))); err != nil {
					t.Fatalf("%s did not demand-load helpers: %v", markup, err)
				}
				if strings.HasPrefix(name, "invalid") {
					if name == "invalid-button" {
						var disabled bool
						if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('button')[1].disabled`, &disabled)); err != nil || disabled {
							t.Fatalf("unmarked sibling disabled=%v, err=%v", disabled, err)
						}
					}
					if err := chromedp.Run(ctx,
						chromedp.Poll(`document.querySelector('button').disabled`, nil, chromedp.WithPollingTimeout(2*time.Second)),
						chromedp.Evaluate(`(() => { const field = document.querySelector('input'); field.value = 'valid'; field.dispatchEvent(new Event('input', {bubbles:true})); })()`, nil),
						chromedp.Poll(`!document.querySelector('button').disabled`, nil),
					); err != nil {
						t.Fatalf("invalid-state helper did not follow form validity: %v", err)
					}
				}
			})
		}
	}
}
