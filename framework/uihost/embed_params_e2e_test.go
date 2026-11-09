package uihost

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// tallParamComp renders the param its route bound, then enough rows that
// the frame grows past the loader's 150px start: the parent sees the
// content arrive without reaching into the cross-origin frame.
type tallParamComp struct{ value string }

func (c *tallParamComp) SetParams(p map[string]string) { c.value = p["p"] }
func (c *tallParamComp) Render() render.HTML {
	var b strings.Builder
	b.WriteString("<p>p:" + stdhtml.EscapeString(c.value) + "</p>")
	for i := range 40 {
		fmt.Fprintf(&b, "<p>row %d</p>", i)
	}
	return render.HTML(b.String())
}

// The loader puts a dynamic surface's path on the frame URL, each segment
// encoded, from data-path and from mount({path}) alike. A file name with a
// space, "#" and "?" must reach the route as one segment: unencoded, "#"
// ends the URL and "?" starts its query.
//
// The spacer puts the second frame below the fold from the start. Chrome
// stops rendering a cross-origin frame there, so a height report that
// waits for the next animation frame never arrives; CI hit this whenever
// the first frame grew before the second had reported.
func TestEmbedDynamicPathInABrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	const file = "notes #1?.txt"
	var appURL, snippetNonce, mountNonce string
	customerMux := http.NewServeMux()
	customerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>customer</title></head><body>
<h1 id="host-page">Acme</h1>
<div id="snippet"></div><div style="height:3000px"></div><div id="mounted"></div>
<script src="%[1]s/__gofastr/embed.js" data-surface="files" data-path="/files/a/%[3]s" data-token="%[2]s" data-target="#snippet"></script>
<script>GoFastrEmbed.mount({surface: 'files', path: %[5]q, token: %[4]q, target: '#mounted'});</script>
</body></html>`, appURL, snippetNonce, file, mountNonce, "/files/b/"+file)
	})
	customer := httptest.NewServer(customerMux)
	t.Cleanup(customer.Close)

	application := app.NewApp("Embed Params E2E")
	files := app.NewScreen("/files/{p...}", &tallParamComp{})
	application.RegisterScreen(files, app.EmbedLayout())
	eh, err := fembed.New(fembed.Config{
		Surfaces:  []fembed.Surface{{Name: "files", Screen: files, Origins: []string{customer.URL}}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	ds := New(application, WithEmbed(eh))

	var mu sync.Mutex
	var contentPaths []string
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			mu.Lock()
			contentPaths = append(contentPaths, r.URL.Query().Get("path"))
			mu.Unlock()
		}
		ds.ServeHTTP(w, r)
	}))
	t.Cleanup(appSrv.Close)
	appURL = appSrv.URL
	for _, n := range []*string{&snippetNonce, &mountNonce} {
		if *n, err = eh.MintNonce(context.Background(), "files", "user-7", customer.URL, nil); err != nil {
			t.Fatalf("MintNonce: %v", err)
		}
	}

	ctx := chromedptest.Context(t)
	var srcs []string
	grown := `(() => [...document.querySelectorAll('iframe')].filter(f => f.getBoundingClientRect().height > 200).length)()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(customer.URL+"/"),
		chromedp.WaitVisible(`#host-page`, chromedp.ByID),
		chromedp.Poll(grown+` === 2`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('iframe')].map(f => new URL(f.src).pathname)`, &srcs),
	); err != nil {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("chromedp: %v (content paths %q)", err, contentPaths)
	}
	const enc = "notes%20%231%3F.txt"
	want := map[string]bool{
		"/__gofastr/embed/files/files/a/" + enc: true,
		"/__gofastr/embed/files/files/b/" + enc: true,
	}
	for _, s := range srcs {
		if !want[s] {
			t.Errorf("frame src path %q, want one of %v", s, want)
		}
		delete(want, s)
	}
	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, p := range contentPaths {
		got[p] = true
	}
	for _, p := range []string{"/files/a/" + enc, "/files/b/" + enc} {
		if !got[p] {
			t.Errorf("no content request for ?path=%s; content paths %q", p, contentPaths)
		}
	}
}
