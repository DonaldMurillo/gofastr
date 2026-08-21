package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// segmentedRadio emits the exact <input type="radio"> markup the
// framework/ui.SegmentedControl component renders for one option (see
// framework/ui/segmented.go). Keeping the runtime-level fixture
// byte-equivalent to the component output means this test exercises the
// real dispatch path without crossing the core-ui → framework layer line;
// framework/ui/segmented_test.go pins that the component emits these
// attributes. rpc.js is the unit under test here, not the component.
const segmentedRadio = `
<input type="radio" name="%s" value="%s" class="ui-segmented__input" id="%s--%s"
       data-fui-rpc="%s" data-fui-rpc-method="POST"%s>`

func segmentedPage(form bool, extraInputAttrs, signalSpan string) string {
	var signalAttr string
	if signalSpan != "" {
		signalAttr = ` data-fui-rpc-signal="` + signalSpan + `"`
	}
	radio := func(val string) string {
		return fmt.Sprintf(segmentedRadio, "plan", val, "plan", val, "/plan/set", signalAttr+extraInputAttrs)
	}
	open, closeTag := "<div>", "</div>"
	if form {
		open, closeTag = `<form id="picker">`, `</form>`
	}
	echo := "single"
	if signalSpan == "" {
		echo = ""
	}
	return fmt.Sprintf(`<!doctype html><html><head><title>seg</title></head><body>
%s
  <div class="ui-segmented" role="radiogroup" aria-label="Plan" data-count="2">
    <label class="ui-segmented__option" for="plan--single" data-position="0">%s<span>Single machine</span></label>
    <label class="ui-segmented__option" for="plan--unlimited" data-position="1">%s<span>Unlimited machines</span></label>
  </div>
%s
<p>Chosen: <span id="echo" data-fui-signal="plan-echo" data-fui-signal-mode="text">%s</span></p>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, open, radio("single"), radio("unlimited"), closeTag, echo)
}

// planSetHandler records the decoded JSON body and replies with the chosen
// plan value, falling back to the SAME confident default a real handler
// uses when the field is missing. Before the fix the body is empty, so the
// handler reads plan="" and renders "single" even after the operator clicks
// "Unlimited machines": the bug is not a missing request, it is a request
// that arrives without the selection, producing a confidently wrong answer.
func planSetHandler(t *testing.T, mu *sync.Mutex, recorded *map[string]string, hits *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]string
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m)
		}
		mu.Lock()
		*hits++
		*recorded = m
		mu.Unlock()
		chosen := m["plan"]
		if chosen == "" {
			chosen = "single" // the confidently-wrong default the empty-body bug produces
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, chosen)
	}
}

// TestSegmentedControl_RPCPostsSelectedValue is the core proof for #194: a
// SegmentedControl radio inside a <form> with RPCPath must POST a body
// carrying the CHOSEN segment's name=value so the handler renders the right
// value, not the default. The old rpc.js only serialized a FORM node, so a
// radio dispatched with an empty body and the handler fell through to its
// default, clicking "Unlimited machines" rendered "single".
func TestSegmentedControl_RPCPostsSelectedValue(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var recorded map[string]string
	hits := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/plan/set", planSetHandler(t, &mu, &recorded, &hits))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, segmentedPage(true, "", "plan-echo"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var echo string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Click the "Unlimited machines" radio. The browser checks it before
		// the click event fires, so new FormData(radio.form) carries plan=unlimited.
		chromedp.Click(`#plan--unlimited`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Text(`#echo`, &echo, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("plan RPC hit %d time(s), want 1", hits)
	}
	got := recorded["plan"]
	if got != "unlimited" {
		t.Errorf("server received plan=%q, want \"unlimited\" — the radio's selection did not round-trip in the body (recorded=%v)", got, recorded)
	}
	// The echo is the user-visible symptom: before the fix the empty body made
	// the handler render its default, so the operator saw "single" after
	// clicking "Unlimited machines". Assert the CHOSEN value renders.
	if echo != "unlimited" {
		t.Errorf("echo rendered %q, want \"unlimited\" — the server echoed its default because the selection never arrived", echo)
	}
}

// TestSegmentedControl_RPCExplicitBodyWins pins the precedence contract:
// an explicit data-fui-rpc-body must still win over form serialization so
// existing callers that hand-craft a JSON body are not regressed. This test
// passes before AND after the fix, it is a regression guard for the
// precedence the fix must preserve.
func TestSegmentedControl_RPCExplicitBodyWins(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var recorded map[string]string
	hits := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/plan/set", planSetHandler(t, &mu, &recorded, &hits))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// data-fui-rpc-body on the radio; the form also carries a plan field
		// whose value differs, so the assertion can tell which source won.
		fmt.Fprint(w, segmentedPage(true, ` data-fui-rpc-body='{"plan":"override"}'`, ""))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#plan--unlimited`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("plan RPC hit %d time(s), want 1", hits)
	}
	if recorded["plan"] != "override" {
		t.Errorf("plan=%q, want \"override\" — explicit data-fui-rpc-body must win over form serialization", recorded["plan"])
	}
}

// TestSegmentedControl_RPCNoFormDoesNotError pins requirement #3: a form
// control with NO enclosing form (node.form === null) and no explicit body
// keeps today's behaviour, the request fires with an empty body and the
// runtime must not throw serializing a null form. This passes before AND
// after the fix; it guards against the fix introducing a FormData(null) throw.
func TestSegmentedControl_RPCNoFormDoesNotError(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var recorded map[string]string
	hits := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/plan/set", planSetHandler(t, &mu, &recorded, &hits))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// No <form> wrapper: node.form is null on every radio. A page-level
		// error recorder runs BEFORE the runtime so a FormData(null) throw is
		// observable, hit==1 alone would not distinguish "no body" from "threw
		// before fetch".
		fmt.Fprint(w, `<!doctype html><html><head><title>seg-noform</title></head><body>
<script>window.__pageErrors=[];window.addEventListener('error',function(e){window.__pageErrors.push(e.message||String(e));});</script>
<div class="ui-segmented" role="radiogroup" aria-label="Plan" data-count="2">
  <label class="ui-segmented__option" for="plan--single" data-position="0">
    <input type="radio" name="plan" value="single" class="ui-segmented__input" id="plan--single" data-fui-rpc="/plan/set" data-fui-rpc-method="POST" data-fui-rpc-signal="plan-echo" checked>
    <span>Single machine</span>
  </label>
  <label class="ui-segmented__option" for="plan--unlimited" data-position="1">
    <input type="radio" name="plan" value="unlimited" class="ui-segmented__input" id="plan--unlimited" data-fui-rpc="/plan/set" data-fui-rpc-method="POST" data-fui-rpc-signal="plan-echo">
    <span>Unlimited machines</span>
  </label>
</div>
<span id="echo" data-fui-signal="plan-echo" data-fui-signal-mode="text">single</span>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var pageErrs []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#plan--unlimited`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__pageErrors`, &pageErrs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("plan RPC hit %d time(s), want 1 — a form control with no enclosing form must still dispatch (empty body), not error", hits)
	}
	if len(pageErrs) > 0 {
		t.Errorf("unexpected page error(s) dispatching a form-control RPC with no form: %v", pageErrs)
	}
	// No form → legacy empty body. The recorded map has no plan; that is
	// today's documented behaviour, NOT a regression.
	if plan, ok := recorded["plan"]; ok && plan != "" {
		t.Errorf("no-form control posted plan=%q unexpectedly; a control with no enclosing form must keep the legacy empty body", plan)
	}
}

// TestKiln_RPCFormControlCarriesValue covers the SIBLING dispatcher
// (_dispatchKiln) for the same class of bug: a data-kiln-tool form control
// inside a <form> must carry its value, not post ” (data-kiln-args). The
// kiln path only serializes a FORM node today, so a radio posts an empty
// JSON body. Explicit data-kiln-args still wins; this uses a bare control
// so the new form-serialization branch is the one under test.
func TestKiln_RPCFormControlCarriesValue(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var recorded map[string]string
	hits := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/kiln/tool/set-plan", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]string
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m)
		}
		mu.Lock()
		hits++
		recorded = m
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// kiln-app body class satisfies _kilnOK so _dispatchKiln runs.
		fmt.Fprint(w, `<!doctype html><html><head><title>kiln</title></head><body class="kiln-app">
<form id="picker">
  <div class="ui-segmented" role="radiogroup" aria-label="Plan" data-count="2">
    <label class="ui-segmented__option" for="plan--single" data-position="0">
      <input type="radio" name="plan" value="single" class="ui-segmented__input" id="plan--single" data-kiln-tool="set-plan" checked>
      <span>Single machine</span>
    </label>
    <label class="ui-segmented__option" for="plan--unlimited" data-position="1">
      <input type="radio" name="plan" value="unlimited" class="ui-segmented__input" id="plan--unlimited" data-kiln-tool="set-plan">
      <span>Unlimited machines</span>
    </label>
  </div>
</form>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#plan--unlimited`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("kiln tool hit %d time(s), want 1", hits)
	}
	if recorded["plan"] != "unlimited" {
		t.Errorf("kiln tool received plan=%q, want \"unlimited\" — a data-kiln-tool form control must serialize its form, not post '' (recorded=%v)", recorded["plan"], recorded)
	}
}
