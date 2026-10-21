package runtime

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A soft navigation keeps the document, so the runtime's own words must
// follow the destination's reader: a partial carries them on its
// page-head template (data-cui-strings), a full fetch in its
// #gofastr-strings block, and a page with neither is English.
//
//	/s/en, /s/es → one shared shell (l:site): partial swaps
//	/k/en, /k/es → per-language keyed shells: full fetches
const stringsNavRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/s/en","layouts":["l:site"]},{"path":"/s/es","layouts":["l:site"]},` +
	`{"path":"/k/en","layouts":["l:k-en"]},{"path":"/k/es","layouts":["l:k-es"]}` +
	`]</script>`

const stringsES = `{"runtime.load_failed":"No se pudo cargar {{path}}"}`

func stringsNavPage(lang, key, strs, id, next string) string {
	block := ""
	if strs != "" {
		block = `<script type="application/json" id="gofastr-strings">` + strs + `</script>`
	}
	return `<!doctype html><html lang="` + lang + `"><head><title>` + id + `</title>` + stringsNavRoutes + block + `</head><body>` +
		`<div data-cui-layout="site" data-cui-layout-key="` + key + `" data-cui-lang="` + lang + `">` +
		`<main role="main" tabindex="-1" data-cui-layout-slot="` + key + `">` +
		stringsNavContent(id, next) +
		`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
}

func stringsNavContent(id, next string) string {
	return `<h1 id="` + id + `">` + id + `</h1><a id="go-` + id + `" href="` + next + `">next</a>`
}

func stringsNavPartial(lang, strs, id, next string) string {
	attr := ""
	if strs != "" {
		attr = ` data-cui-strings="` + html.EscapeString(strs) + `"`
	}
	return `<template data-cui-page-head="" data-cui-lang="` + lang + `"` + attr + `></template>` + stringsNavContent(id, next)
}

func TestNavSwapsRuntimeStrings(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	shared := func(lang, strs, id, next string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", id)
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprint(w, stringsNavPartial(lang, strs, id, next))
				return
			}
			fmt.Fprint(w, stringsNavPage(lang, "l:site", strs, id, next))
		}
	}
	mux.HandleFunc("/s/en", shared("en", "", "s-en", "/s/es"))
	mux.HandleFunc("/s/es", shared("es", stringsES, "s-es", "/s/en"))
	mux.HandleFunc("/k/en", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, stringsNavPage("en", "l:k-en", "", "k-en", "/k/es"))
	})
	mux.HandleFunc("/k/es", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, stringsNavPage("es", "l:k-es", stringsES, "k-es", "/k/en"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const probe = `__gofastr.say('runtime.load_failed', 'Could not load {{path}}', {path: '/x'})`
	for _, base := range []string{"/s", "/k"} {
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		var en1, es, en2 string
		id := base[1:]
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+base+"/en"),
			chromedp.WaitVisible(`#`+id+`-en`, chromedp.ByID),
			chromedp.Evaluate(probe, &en1),
			chromedp.Click(`#go-`+id+`-en`, chromedp.ByID),
			chromedp.WaitVisible(`#`+id+`-es`, chromedp.ByID),
			chromedp.Evaluate(probe, &es),
			chromedp.Click(`#go-`+id+`-es`, chromedp.ByID),
			chromedp.WaitVisible(`#`+id+`-en`, chromedp.ByID),
			chromedp.Evaluate(probe, &en2),
		); err != nil {
			t.Fatalf("%s: chromedp: %v", base, err)
		}
		if en1 != "Could not load /x" || es != "No se pudo cargar /x" || en2 != "Could not load /x" {
			t.Errorf("%s: say across en → es → en = %q / %q / %q", base, en1, es, en2)
		}
	}
}
