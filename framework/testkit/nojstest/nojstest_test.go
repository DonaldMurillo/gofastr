package nojstest

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

const cleanPage = `<!doctype html><html><body>
<main>
  <p>Hello without script.</p>
  <a href="/next">Next</a>
  <a aria-current="page">Here</a>
  <form action="/save" method="post"><input name="a"><input name="b"><button>Save</button></form>
  <form action="/save" method="post"><input name="a"><input name="b"><button type="primary">Odd type</button></form>
  <form action="/search"><input type="search" name="q"></form>
  <form method="dialog"><input name="x"><input name="y"></form>
  <form action="/save" method="post"><input name="a"><input name="b"><input type="image" src="data:," alt="Save"></form>
  <form action="/save" method="post"><input name="elements"><input name="b"><button>Save</button></form>
  <form action="#"><input name="q"><input name="r"><button>Search here</button></form>
  <div style="display:none"><a href="#">hidden toggle</a></div>
</main>
<script>window.__gofastr = {};</script>
</body></html>`

const brokenPage = `<!doctype html><html><body>
<nav>
  <a href="#">Menu</a>
  <a href="javascript:void(0)">Open</a>
  <a role="button">Do it</a>
  <a href=" java&#9;script:void(0)">Tab</a>
  <form action="java&#10;script:x" method="post"><input name="a"><button>Go</button></form>
  <form action="/save" method="post"><input name="a"><input name="b"><button type="button">Send</button></form>
  <form action="/save" method="post"><input name="a"><input name="b"><button disabled>Off</button></form>
  <form action="/find"><input name="q"><button disabled>Go</button></form>
  <form action="/save" method="post"><fieldset disabled><input name="a"><input name="b"><button>Off</button></fieldset></form>
</nav>
<main><div style="height:2em"></div></main>
</body></html>`

// The text is in the DOM but the visitor sees none of it.
const hiddenMainPage = `<!doctype html><html><body><main hidden><p>Server-rendered content.</p></main></body></html>`

func serve(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range map[string]string{"/clean": cleanPage, "/broken": brokenPage, "/missing": cleanPage, "/hidden": hiddenMainPage} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if path == "/missing" {
				w.WriteHeader(http.StatusNotFound)
			}
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func kinds(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Kind)
	}
	return out
}

func TestCleanPagePasses(t *testing.T) {
	base := serve(t)
	tab, cancel := NewTab(t, chromedptest.Context(t))
	defer cancel()
	ps, err := Check(tab, base+"/clean")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Fatalf("clean page reported %v", ps)
	}
}

func TestBrokenPageReportsEachKind(t *testing.T) {
	base := serve(t)
	tab, cancel := NewTab(t, chromedptest.Context(t))
	defer cancel()
	ps, err := Check(tab, base+"/broken")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"empty-main", "dead-link", "dead-link", "dead-link", "dead-link", "form-no-submit", "form-no-submit", "form-no-submit", "form-no-submit", "form-no-submit"}
	if got := kinds(ps); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v\n%v", got, want, ps)
	}
}

func TestHiddenMainIsEmpty(t *testing.T) {
	base := serve(t)
	tab, cancel := NewTab(t, chromedptest.Context(t))
	defer cancel()
	ps, err := Check(tab, base+"/hidden")
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(ps); !slices.Equal(got, []string{"empty-main"}) {
		t.Fatalf("got %v, want [empty-main]", ps)
	}
}

func TestErrorStatusIsReported(t *testing.T) {
	base := serve(t)
	tab, cancel := NewTab(t, chromedptest.Context(t))
	defer cancel()
	ps, err := Check(tab, base+"/missing")
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(ps); !slices.Equal(got, []string{"http-status"}) {
		t.Fatalf("got %v, want [http-status]", ps)
	}
}

// With script left on, the page's own script sets window.__gofastr and
// Inspect must say so: the guard that keeps a gate from passing while
// the runtime quietly ran.
func TestScriptRanIsReported(t *testing.T) {
	base := serve(t)
	tab, cancel := chromedp.NewContext(chromedptest.Context(t))
	defer cancel()
	if err := chromedp.Run(tab, chromedp.Navigate(base+"/clean")); err != nil {
		t.Fatal(err)
	}
	ps, err := Inspect(tab)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(ps); !slices.Equal(got, []string{"script-ran"}) {
		t.Fatalf("got %v, want [script-ran]", ps)
	}
}
