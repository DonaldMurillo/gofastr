//go:build chromium

package sitesearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// parityCorpus mixes what ranking has to tell apart: a term in a title,
// in a heading and in body text, camelCase identifiers, accented and
// CJK words, repeated terms past the tf cap, and long sections whose
// excerpts cut both ends.
func parityCorpus() []Page {
	long := strings.Repeat("Filler words keep this section long enough to cut. ", 12)
	return []Page{
		{URL: "/docs/forms", Title: "Forms", Group: "Docs", Sections: []Section{
			{Text: "Build a form from FormField parts. " + long},
			{ID: "validation", Heading: "Validation", Text: long + "Errors render beside each field; validation runs on the server. " + long},
			{ID: "server-errors", Heading: "Server errors", Text: "The server returns a field map of errors."},
		}},
		{URL: "/docs/palette", Title: "Command palette", Group: "Docs", Sections: []Section{
			{Text: "ui.CommandPalette opens on ⌘K. Palette palette palette palette."},
			{ID: "search", Heading: "Search", Text: "Full-text search over every rendered page."},
		}},
		{URL: "/components/table", Title: "DataTable", Group: "Components", Sections: []Section{
			{Text: "A sortable table with pagination. Café crème and naïve résumé rows."},
			{ID: "columns", Heading: "Columns", Text: "Columns declare a renderer; the server sorts."},
		}},
		{URL: "/blog/日本", Title: "日本語 support", Sections: []Section{
			{Text: "Unicode words like 日本語 and Straße index as letters."},
		}},
		// JS's toLowerCase is contextual (a final sigma) and expands İ;
		// Go lowercases rune by rune.
		{URL: "/blog/greek", Title: "ΟΣ ΚΟΣΜΟΣ", Sections: []Section{
			{Text: "İSTANBUL and İzmir, in capitals."},
		}},
		{URL: "/examples/tracker", Title: "Tracker", Group: "Examples", Sections: []Section{
			{Text: "An issue tracker with forms, a palette and server-rendered tables."},
		}},
		// The title tier: a title the query names whole, one it only
		// begins, and a longer title that holds it.
		{URL: "/components/authcard", Title: "AuthCard", Group: "Components", Sections: []Section{
			{Text: "A sign-in card. Auth for auth pages."},
		}},
		{URL: "/docs/auth", Title: "Authentication", Group: "Docs", Sections: []Section{
			{Text: "Install github.com/DonaldMurillo/gofastr-plugins/formbuilder first. " + long + "Then a form signs the session in."},
		}},
		{URL: "/components/toc", Title: "TableOfContents", Group: "Components", Sections: []Section{
			{Text: "A table of contents for long pages."},
		}},
		{URL: "/components/themepicker", Title: "ThemePicker", Sections: []Section{
			{Text: "Pick a theme; the theme sticks."},
		}},
		{URL: "/docs/theming", Title: "Theming", Sections: []Section{
			{Text: "Tokens drive caching and colors."},
		}},
		// Title slots: a camelCase title named whole against one word
		// the query only begins, and two terms against one title word.
		// The Formatter text puts a prefix hit before a whole one, so
		// the excerpt anchor must not depend on term order.
		{URL: "/blog/datatables", Title: "Datatables", Sections: []Section{
			{Text: "Notes on datatable layouts."},
		}},
		{URL: "/docs/formatter", Title: "Formatter", Sections: []Section{
			{Text: "Formatters reshape values. " + long + "A form uses one to format dates."},
		}},
		{URL: "/docs/form-format", Title: "Form format options", Sections: []Section{
			{Text: "How a form picks its format."},
		}},
		// A two-letter word matches only itself: go, never gofastr.
		{URL: "/docs/go", Title: "Go", Sections: []Section{
			{Text: "Written in Go, the gofastr way; go install it."},
		}},
	}
}

var parityQueries = []string{
	"forms", "form", "fo", "valid", "server", "server errors", "palette",
	"commandpalette", "command pal", "table", "tab", "café", "cafe", "résumé",
	"日本語", "straße", "errors field", "zzz", "forms zzz", "f", "",
	"Pagination", "the", "renders", "palette search", "columns sort",
	"ΟΣ", "οσ", "κοσμοσ", "ΚΟΣΜΟΣ", "İSTANBUL", "istanbul", "izmir",
	"auth", "authentication", "data table", "tables", "formbuilder", "sign forms",
	"theme", "theming", "cache", "themes", "free",
	"datatable", "form format", "format form", "formatter", "form formatters",
	"go", "gof", "go install",
}

// The browser ranks as Go does over the shipped shards, and over
// shards split down to single terms, where a query term has to gather
// its matches from every split shard under its prefix.
func TestStaticSearchMatchesGo(t *testing.T) {
	idx := Build(parityCorpus())
	t.Run("shipped", func(t *testing.T) {
		files, err := idx.Files()
		if err != nil {
			t.Fatal(err)
		}
		if n := countShards(files); n < 5 {
			t.Fatalf("%d shards: the corpus no longer spans keys", n)
		}
		checkParity(t, idx, files)
	})
	t.Run("split", func(t *testing.T) {
		files, err := idx.files(1)
		if err != nil {
			t.Fatal(err)
		}
		shipped, _ := idx.Files()
		if countShards(files) <= countShards(shipped) {
			t.Fatalf("splitting made %d shards, shipped %d", countShards(files), countShards(shipped))
		}
		checkParity(t, idx, files)
	})
}

func countShards(files map[string][]byte) int {
	n := 0
	for f := range files {
		if strings.HasPrefix(f, "t-") {
			n++
		}
	}
	return n
}

func checkParity(t *testing.T, idx *Index, files map[string][]byte) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html data-cui-static><meta charset=utf-8><script src="/client.js"></script><script src="/static.js"></script>`)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/static.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, staticJS)
	})
	mux.HandleFunc(Root+"{file}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.PathValue("file")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := browser(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._sitesearch && window.__gofastr._sitesearch.staticSearch)`, nil)); err != nil {
		t.Fatalf("load client: %v", err)
	}

	nonEmpty := 0
	for _, q := range parityQueries {
		want := normalize(idx.Search(q, 8))
		var raw string
		expr := `window.__gofastr._sitesearch.staticSearch(` + jsString(q) + `, 8).then(r => JSON.stringify(r))`
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		})); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		var got []Result
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("query %q: decode %s: %v", q, raw, err)
		}
		got = normalize(got)
		if !reflect.DeepEqual(got, want) {
			g, _ := json.MarshalIndent(got, "", " ")
			w, _ := json.MarshalIndent(want, "", " ")
			t.Errorf("query %q: browser ranked\n%s\nGo ranked\n%s", q, g, w)
		}
		if len(want) > 0 {
			nonEmpty++
		}
	}
	// Anti-vacuity: most queries must return something, or the parity
	// is two empty lists agreeing.
	if nonEmpty < len(parityQueries)-7 {
		t.Errorf("only %d of %d queries returned results", nonEmpty, len(parityQueries))
	}
}

func normalize(rs []Result) []Result {
	if len(rs) == 0 {
		return nil
	}
	for i := range rs {
		if len(rs[i].Sections) == 0 {
			rs[i].Sections = nil
		}
		if len(rs[i].Excerpt) == 0 {
			rs[i].Excerpt = nil
		}
		for j := range rs[i].Sections {
			if len(rs[i].Sections[j].Excerpt) == 0 {
				rs[i].Sections[j].Excerpt = nil
			}
		}
	}
	return rs
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestClientShowsLoadingAndEmptyRows: a slow answer shows the listbox's
// loading word until the results land, and a query nothing matches
// shows its empty word, so the list is never blank while a reader
// waits.
func TestClientShowsLoadingAndEmptyRows(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><meta charset=utf-8>
<div data-hui-combobox><input data-hui-combobox-input id="q" aria-controls="lb" aria-expanded="false">
<ul id="lb" role="listbox" hidden data-search data-search-loading="Loading…" data-search-empty="No matches"></ul></div>
<script src="/client.js"></script>`)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc(Root+"query", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("q") == "slow" {
			time.Sleep(time.Second)
			fmt.Fprint(w, `{"results":[{"url":"/a","title":"Slow page"}]}`)
			return
		}
		fmt.Fprint(w, `{"results":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := browser(t)

	rows := `[...document.querySelectorAll('#lb [data-search-result]')].map(li => li.textContent).join('|')`
	var loading, loaded, empty string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._sitesearch)`, nil),
		chromedp.SendKeys(`#q`, "slow", chromedp.ByID),
		chromedp.Poll(rows+` === 'Loading…'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &loading),
		chromedp.Poll(rows+`.startsWith('Slow page')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &loaded),
		chromedp.Evaluate(`document.getElementById("q").value = ""`, nil),
		chromedp.SendKeys(`#q`, "zzqx", chromedp.ByID),
		chromedp.Poll(rows+` === 'No matches'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &empty),
	); err != nil {
		t.Fatalf("rows: %v (loading %q, loaded %q, empty %q)", err, loading, loaded, empty)
	}
}

// An exported page has no query endpoint: the first query asks the
// kernel for the ranker module by its registered name, then ranks in
// the browser.
func TestExportedPageLoadsRanker(t *testing.T) {
	files, err := Build(parityCorpus()).Files()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html data-cui-static><meta charset=utf-8>
<div data-hui-combobox><input data-hui-combobox-input id="q" aria-controls="lb" aria-expanded="false">
<ul id="lb" role="listbox" hidden data-search></ul></div>
<script>window.__gofastr = {loadModule: function (n) {
  window.asked = n;
  return new Promise(function (ok) {
    const s = document.createElement('script');
    s.src = '/' + n + '.js';
    s.onload = ok;
    document.head.append(s);
  });
}};</script>
<script src="/client.js"></script>`)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/sitesearch-static.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, staticJS)
	})
	mux.HandleFunc(Root+"{file}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.PathValue("file")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := browser(t)

	rows := `[...document.querySelectorAll('#lb [data-search-result] .fui-cmd-palette__option-label')].map(s => s.textContent).join('|')`
	var got, asked string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr._sitesearch`, nil),
		chromedp.SendKeys(`#q`, "server errors", chromedp.ByID),
		chromedp.Poll(rows+` !== ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &got),
		chromedp.Evaluate(`window.asked`, &asked),
	); err != nil {
		t.Fatalf("rows: %v (got %q)", err, got)
	}
	if !strings.HasPrefix(got, "Forms|Server errors") {
		t.Errorf("rows = %q, want Forms then its Server errors section", got)
	}
	e, ok := registry.LookupBehavior(asked)
	if !ok || e.Source != staticJS {
		t.Errorf("the client asked for module %q, which is not the registered ranker", asked)
	}
}

// The module loads when the palette first opens, so a fast reader can
// finish typing before it arrives; that query still gets answered.
func TestClientAnswersEarlyQuery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><meta charset=utf-8>
<div data-hui-combobox><input data-hui-combobox-input id="q" aria-controls="lb" aria-expanded="false" value="forms">
<ul id="lb" role="listbox" hidden data-search></ul></div>
<script>document.getElementById('q').focus()</script>
<script src="/client.js"></script>`)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc(Root+"query", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"results":[{"url":"/docs/forms","title":"Found %s"}]}`, r.URL.Query().Get("q"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := browser(t)

	rows := `[...document.querySelectorAll('#lb [data-search-result]')].map(li => li.textContent).join('|')`
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(rows+` !== ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows, &got),
	); err != nil {
		t.Fatalf("the query typed before the module loaded got no rows: %v", err)
	}
	if got != "Found forms" {
		t.Errorf("rows = %q, want Found forms", got)
	}
}

// browser starts a headless Chrome that lives for the test.
func browser(t *testing.T) context.Context {
	t.Helper()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WSURLReadTimeout(90*time.Second), chromedp.NoSandbox)...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancelTimeout)
	return ctx
}

// A result that is still in place after the next keystroke keeps its
// row, so its entrance does not replay; a result that changed gets a
// new row.
func TestClientKeepsRowsThatStay(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><meta charset=utf-8>
<div data-hui-combobox><input data-hui-combobox-input id="q" aria-controls="lb" aria-expanded="false">
<ul id="lb" role="listbox" hidden data-search></ul></div>
<script src="/client.js"></script>`)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc(Root+"query", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query().Get("q")
		fmt.Fprintf(w, `{"results":[{"url":"/docs/forms","title":"Forms %s"},{"url":"/%s","title":"Other %s"}]}`, q, q, q)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := browser(t)

	rows := `[...document.querySelectorAll('#lb [data-search-result]')]`
	var kept []bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr._sitesearch)`, nil),
		chromedp.SendKeys(`#q`, "fo", chromedp.ByID),
		chromedp.Poll(rows+`.length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows+`.forEach(li => li.mark = true)`, nil),
		chromedp.SendKeys(`#q`, "r", chromedp.ByID),
		chromedp.Poll(rows+`[0].textContent === 'Forms for'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(rows+`.map(li => !!li.mark)`, &kept),
	); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !reflect.DeepEqual(kept, []bool{true, false}) {
		t.Errorf("rows kept %v, want the unchanged first row kept and the changed second one new", kept)
	}
}
