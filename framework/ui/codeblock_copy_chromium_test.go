package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A framed CodeBlock renders one display:block span per line, and an
// empty block contributes nothing to innerText or to a selection's
// text, so both the copy button and a reader's own select-and-copy
// (the no-script path) dropped every blank line of the sample. Both
// per-line paths are covered: Lines, and Code with a per-line feature.
func TestCodeBlockCopyKeepsBlankLines(t *testing.T) {
	page := string(CodeBlock(CodeBlockConfig{
		ExtraAttrs:  html.Attrs{"data-cb": "lines"},
		Filename:    "main.go",
		ShowCopy:    true,
		LineNumbers: true,
		Lines:       []render.HTML{render.Text("a := 1"), "", render.Text("b := 2")},
	})) + string(CodeBlock(CodeBlockConfig{
		ExtraAttrs:     html.Attrs{"data-cb": "code"},
		Filename:       "main.go",
		ShowCopy:       true,
		LineNumbers:    true,
		HighlightLines: []LineRange{{From: 3}},
		Code:           "a := 1\n\nb := 2\n",
	}))
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	css := codeBlockStyle.Entry().CSSFor(theme.Default())
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := runtime.Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset=utf-8><style>%s</style>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`%s<script src="/__gofastr/runtime.js"></script></body></html>`, css, runtime.BehaviorsJSON(), page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	// The clipboard is stubbed: the assertion is the string the handler
	// hands to writeText, not the OS clipboard.
	const stub = `Object.defineProperty(navigator, 'clipboard', {configurable: true,
		value: {writeText: function (s) { window.__copied = s; return Promise.resolve(); }}}); true`
	const want = "a := 1\n\nb := 2"
	for _, name := range []string{"lines", "code"} {
		var copied, selected string
		sel := `[data-cb="` + name + `"]`
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL),
			chromedp.WaitVisible(sel+` [data-hui-copy] button`, chromedp.ByQuery),
			chromedp.Evaluate(stub, nil),
			chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-feedback'])`,
				nil, chromedp.WithPollingTimeout(15*time.Second)),
			chromedp.Click(sel+` [data-hui-copy] button`, chromedp.ByQuery),
			chromedp.Poll(`typeof window.__copied === 'string'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`window.__copied`, &copied),
			chromedp.Evaluate(`(() => { const s = getSelection(); s.removeAllRanges();
				s.selectAllChildren(document.querySelector('`+sel+` pre')); return s.toString().replace(/\n+$/, ''); })()`, &selected),
		); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if copied != want {
			t.Errorf("%s: copy button wrote %q, want %q", name, copied, want)
		}
		if selected != want {
			t.Errorf("%s: selecting the block reads %q, want %q", name, selected, want)
		}
	}
	// The newline each line span now carries must not draw a row of its
	// own: keep a picture of both blocks for review (go test -artifacts).
	var shot []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "codeblocks.png"), shot, 0o600); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}
}
