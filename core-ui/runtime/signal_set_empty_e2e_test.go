package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// data-cui-signal-set="<name>[:<value>]": the contract says a name with
// no ":<value>" sets the empty string. The click handler set a signal
// only when it found a colon past the first character, so a bare name
// did nothing and the binding kept the old value. A leading colon names
// no signal and still sets nothing.
func TestSignalSetBareNameSetsEmptyString(t *testing.T) {
	srv, _, _ := busyRig(t, `<span id="out" data-cui-signal="draft">typed</span>
<button id="fill" data-cui-signal-set="draft:kept">fill</button>
<button id="clear" data-cui-signal-set="draft">clear</button>
<button id="colon" data-cui-signal-set="draft:">colon</button>
<button id="noname" data-cui-signal-set=":x">noname</button>`)
	ctx := chromedptest.Context(t)
	read := func(what string) (text string, value any) {
		t.Helper()
		var got struct {
			Text  string `json:"text"`
			Value any    `json:"value"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`({text: document.getElementById('out').textContent, value: window.__gofastr.getSignal('draft')})`, &got)); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return got.Text, got.Value
	}
	click := func(id string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Click(`#`+id, chromedp.ByQuery), chromedp.Sleep(100*time.Millisecond)); err != nil {
			t.Fatalf("click %s: %v", id, err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.WaitReady(`#clear`, chromedp.ByQuery),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.getSignal)`, nil, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatal(err)
	}
	click("fill")
	if text, value := read("fill"); text != "kept" || value != "kept" {
		t.Fatalf("after name:value the signal is %v and the binding %q, want kept", value, text)
	}
	click("noname")
	if text, _ := read("noname"); text != "kept" {
		t.Fatalf("a leading colon names no signal, yet the binding reads %q", text)
	}
	click("clear")
	if text, value := read("clear"); text != "" || value != "" {
		t.Errorf("a bare name must set the empty string: signal %v, binding %q", value, text)
	}
	click("fill")
	click("colon")
	if text, value := read("colon"); text != "" || value != "" {
		t.Errorf("name: sets the empty string too: signal %v, binding %q", value, text)
	}
}
