package headless

// Browser coverage for headless-multiselect: the chips follow the
// checkboxes (checked → chip, chip × → unchecked), the chip text is
// the option's LABEL (not its value), and the plain-form submit
// contract holds with the runtime blocked — the field name repeats
// per checked option through a real <form> submission. The
// disclosure half (Escape, aria mirror) is headless-disclosure's and
// is pinned by its own e2e; here we prove the module loads it through
// the Requires declaration.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
)

const multiSelectLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-multiselect'])`
const disclosureForMultiLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-disclosure'])`

func multiSelectBody() string {
	return `<form id="f" method="post" action="/submit">` + string(MultiSelect(MultiSelectProps{
		ID: "ms", Name: "langs", Label: "Pick languages", Placeholder: "No languages selected",
		Options: []MultiSelectOption{
			{Value: "go", Label: "Go", Selected: true},
			{Value: "cpp", Label: "C++"},
			{Value: "csharp", Label: "C Sharp"},
		},
	}, nil)) + `</form>`
}

func TestE2E_MultiSelectChipsFollowCheckboxes(t *testing.T) {
	b := startBehaviorServer(t, multiSelectBody())
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, multiSelectLoaded) {
		t.Fatal("the multiselect marker never loaded headless-multiselect")
	}
	if !pollTrue(ctx, disclosureForMultiLoaded) {
		t.Fatal("headless-multiselect loaded without headless-disclosure — the Requires declaration is not honoured")
	}

	// First paint: one chip for the pre-checked option, its text the
	// LABEL.
	if !pollTrue(ctx, `document.querySelectorAll('#ms [data-hui-multiselect-chip]').length === 1`) {
		t.Fatal("the pre-checked option never rendered its chip")
	}
	var chips []string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#ms [data-hui-multiselect-chip-text]')).map(function (s) { return s.textContent; })`, &chips),
	); err != nil {
		t.Fatal(err)
	}
	if len(chips) != 1 || chips[0] != "Go" {
		t.Fatalf("initial chips = %v, want [Go] (the label, not the value)", chips)
	}

	// Checking another option adds its chip; the label text is the
	// chip text even when the value is symbol-heavy.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('ms-opt-1').click()`, nil),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#ms [data-hui-multiselect-chip-text]')).map(function (s) { return s.textContent; })`, &chips),
	); err != nil {
		t.Fatal(err)
	}
	if len(chips) != 2 || chips[1] != "C++" {
		t.Fatalf("chips after checking C++ = %v, want [... C++] (the label, not the value cpp)", chips)
	}

	// A chip's × unchecks its option and the chip goes.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#ms [data-hui-multiselect-remove]').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#ms [data-hui-multiselect-chip]').length === 1`) {
		t.Fatal("removing a chip never unchecked its option")
	}
	var checked bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('ms-opt-0').checked`, &checked),
	); err != nil {
		t.Fatal(err)
	}
	if checked {
		t.Fatal("the removed chip's option is still checked")
	}
}

// TestE2E_MultiSelectSubmitsAsAPlainForm proves the no-script
// contract: on a page with NO runtime script at all, the checkbox
// group submits the repeated field name through a real form POST.
func TestE2E_MultiSelectSubmitsAsAPlainForm(t *testing.T) {
	var mu sync.Mutex
	var forms []url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		forms = append(forms, r.PostForm)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><body><main><span id="ready">ready</span>%s</main></body></html>`, multiSelectBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`!!(window.__gofastr && window.__gofastr.loadedModules)`, new(bool)),
		chromedp.Evaluate(`document.getElementById('ms-opt-1').checked = true; document.getElementById('f').submit()`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for range 20 {
		mu.Lock()
		n := len(forms)
		mu.Unlock()
		if n > 0 {
			break
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`1`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(forms) != 1 {
		t.Fatalf("the plain form never submitted (%d submissions)", len(forms))
	}
	got := forms[0]["langs"]
	if len(got) != 2 || got[0] != "go" || got[1] != "cpp" {
		t.Fatalf("the checkbox group submitted langs=%v, want [go cpp] (the pre-checked plus the newly checked)", got)
	}
}
