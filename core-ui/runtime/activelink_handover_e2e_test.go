package runtime

// The activelink ownership handover: a link carrying
// data-cui-match-prefix (ui.Sidebar emits its MatchPath there) is
// activelink's to mark AND CLEAR — a server-rendered first-paint
// aria-current on it must not survive a navigation that moved
// elsewhere, or a kept sidebar shows two lit entries. A link with
// neither the handover nor the module's own .active class (pagination,
// server breadcrumbs) keeps owning its attributes.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func activelinkHandoverPage() string {
	return `<!doctype html><html><head><title>activelink handover</title>
  <nav aria-label="Primary">
    <!-- The kept-sidebar shape: the STALE first-paint mark sits on a
         link the live route is NOT at, beside the handover attribute
         (ui.Sidebar emits data-cui-match-prefix). Landing on /, the
         sweep must clear Other's inherited mark while marking Home. -->
    <a id="home" href="/" data-cui-match-prefix="/">Home</a>
    <a id="other" href="/other" aria-current="page" data-cui-match-prefix="/other">Other</a>
    <!-- A host-owned mark with no handover: pagination's shape. -->
    <a id="owned" href="/owned" aria-current="page">Owned</a>
    <!-- The plain sidebar leaf: no section prefix, its first-paint
         mark handed over by data-cui-activelink (headless.Sidebar
         emits it on every leaf). The handover covers navigations only:
         with none, the server's mark stays (Active on a detail page or
         a query URL). activelink_sweep_e2e_test.go covers the clear. -->
    <a id="leaf" href="/leaf" aria-current="page" data-cui-activelink>Leaf</a>
  </nav>
  <main id="main">ready</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`
}

func activelinkHandoverServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(r.URL.Path[len("/__gofastr/runtime/"):], ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, activelinkHandoverPage())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestActiveLinkClearsHandedOverMarks: the load-time sweep marks the
// current link and clears the STALE first-paint mark a kept layer
// carries (the sidebar shape: aria-current + data-cui-match-prefix),
// while a host-owned mark with no handover attribute keeps its own
// aria-current untouched. Mutation it catches: reverting the clear
// branch to the module's own .active class only leaves the stale SSR
// mark beside the fresh one — two lit entries.
func TestActiveLinkClearsHandedOverMarks(t *testing.T) {
	srv := activelinkHandoverServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#main`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// The idle-loaded module's load-time pass is the sweep under test:
	// it marks Home and must clear Other's stale first-paint mark.
	time.Sleep(600 * time.Millisecond)

	var marks map[string]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const g = (id) => document.getElementById(id).getAttribute('aria-current') || '';
		return { home: g('home'), other: g('other'), owned: g('owned'), leaf: g('leaf') };
	})()`, &marks)); err != nil {
		t.Fatal(err)
	}
	if marks["home"] != "page" {
		t.Errorf("the current link's mark = %q, want page", marks["home"])
	}
	if marks["other"] != "" {
		t.Errorf("the handed-over STALE mark survived on /other (aria-current=%q): a kept sidebar would show two lit entries", marks["other"])
	}
	if marks["owned"] != "page" {
		t.Errorf("the host-owned mark was stripped (aria-current=%q): activelink must clear only what it owns or was handed", marks["owned"])
	}
	if marks["leaf"] != "page" {
		t.Errorf("the load-time sweep cleared a data-cui-activelink leaf with no navigation (aria-current=%q): the server's mark is the truth until one lands", marks["leaf"])
	}
}
