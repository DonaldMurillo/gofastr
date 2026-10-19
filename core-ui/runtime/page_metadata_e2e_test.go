package runtime

import (
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPageMetadataFollowsStaticAndPartialAreaNavigation(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%v", partial), func(t *testing.T) {
			js, err := RuntimeJS()
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, js)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				lang, other, label := "en", "es", "Skip content"
				if r.URL.Path == "/es" {
					lang, other, label = "es", "en", "Saltar contenido"
				}
				head := `<meta data-cui-page-meta="" name="description" content="` + lang + `"><link data-cui-page-meta="" rel="alternate" hreflang="` + other + `" href="/` + other + `">`
				body := `<h1 id="screen-` + lang + `">` + lang + `</h1><a id="next" href="/` + other + `">Next</a>`
				w.Header().Set("Content-Type", "text/html")
				if partial && r.Header.Get("X-Gofastr-Navigate") == "1" {
					w.Header().Set("X-Gofastr-Partial", "true")
					w.Header().Set("X-Gofastr-Envelope", "2")
					w.Header().Set("X-Gofastr-Swap", "l:site")
					carrier := `<template data-cui-page-head="` + stdhtml.EscapeString(head+`<script>window.pageHeadPoison=true</script>`) + `" data-cui-lang="` + lang + `" data-cui-skip-label="` + label + `"></template>`
					fmt.Fprint(w, `<template data-cui-fill="l:site">`+carrier+body+`</template><template data-cui-fill="l:site~header">Header</template>`)
					return
				}
				fmt.Fprint(w, `<!doctype html><html lang="`+lang+`"><head><title>Metadata</title><meta name="global" content="keep">`+head+`<script type="application/json" id="gofastr-routes">[{"path":"/en","layouts":["l:site"]},{"path":"/es","layouts":["l:site"]}]</script></head><body><a data-skip-link href="#main-content">`+label+`</a><div data-cui-layout="site" data-cui-layout-key="l:site" data-cui-lang="`+lang+`"><header data-cui-area="l:site~header">Header</header><main id="main-content" role="main" data-cui-layout-slot="l:site">`+body+`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
			if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/en"), chromedp.Evaluate(`window.stamp='kept'`, nil), chromedp.Click(`#next`),
				chromedp.WaitVisible(`#screen-es`), chromedp.Poll(`document.documentElement.lang==='es' && document.querySelector('meta[name="description"]').content==='es' && document.querySelector('link[hreflang="en"]').getAttribute('href')==='/en' && document.querySelector('[data-skip-link]').textContent==='Saltar contenido' && window.stamp==='kept' && !window.pageHeadPoison`, nil),
				chromedp.Click(`#next`), chromedp.WaitVisible(`#screen-en`), chromedp.Poll(`document.documentElement.lang==='en' && document.querySelector('meta[name="description"]').content==='en' && document.querySelector('link[hreflang="es"]') && document.querySelector('meta[name="global"]').content==='keep'`, nil),
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}
