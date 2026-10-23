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
// #gofastr-strings block, and a page with neither is English. Back
// replays the cached page, which must carry its own words too: the
// live document's head block still holds the first page's.
//
//	/s/en, /s/es → one shared shell (l:site): partial swaps
//	/k/en, /k/es → per-language keyed shells: a full fetch, then a
//	               partial to /k/en2, an English page in the es shell
//	/e/en, /e/es → the shared shell holding an outlet: the envelope
//	               navigator, which snapshots the live document on leave
//	/f/...       → /k's routes under the envelope navigator
//	/h/...       → keyed shells walked en → es → en → en2 → es: es is
//	               left across shells, so no leave capture refreshes its
//	               entry, and en2 re-enters es's shell so the entry
//	               replays from the full fetch's cache
//	/g/...       → /h's routes under the envelope navigator
const stringsNavRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/s/en","layouts":["l:site"]},{"path":"/s/es","layouts":["l:site"]},` +
	`{"path":"/e/en","layouts":["l:site"]},{"path":"/e/es","layouts":["l:site"]},` +
	`{"path":"/k/en","layouts":["l:k-en"]},{"path":"/k/es","layouts":["l:k-es"]},` +
	`{"path":"/k/en2","layouts":["l:k-es"]},` +
	`{"path":"/f/en","layouts":["l:f-en"]},{"path":"/f/es","layouts":["l:f-es"]},` +
	`{"path":"/f/en2","layouts":["l:f-es"]},` +
	`{"path":"/g/en","layouts":["l:g-en"]},{"path":"/g/es","layouts":["l:g-es"]},` +
	`{"path":"/g/en2","layouts":["l:g-es"]},` +
	`{"path":"/h/en","layouts":["l:h-en"]},{"path":"/h/es","layouts":["l:h-es"]},` +
	`{"path":"/h/en2","layouts":["l:h-es"]}` +
	`]</script>`

const stringsES = `{"runtime.load_failed":"No se pudo cargar {{path}}",` +
	`"runtime.request_failed_status":"Error en la solicitud ({{status}})"}`

// rpcErrProbe is what a text-mode signal shows for a failed RPC
// (setSignal with dispatchRPC's {ok:false} object).
const rpcErrProbe = `(() => { const n = document.createElement('span');` +
	` n.setAttribute('data-cui-signal', 'rpcerr'); document.body.appendChild(n);` +
	` __gofastr.setSignal('rpcerr', {ok: false, status: 500, text: 'boom'});` +
	` const t = n.textContent; n.remove(); return t; })()`

func stringsNavPage(lang, key, strs, id, next string) string {
	outlet := ""
	if id[0] == 'e' || id[0] == 'f' || id[0] == 'g' {
		outlet = `<div data-cui-outlet="l:site#aside" id="aside">aside</div>`
	}
	block := ""
	if strs != "" {
		block = `<script type="application/json" id="gofastr-strings">` + strs + `</script>`
	}
	return `<!doctype html><html lang="` + lang + `"><head><title>` + id + `</title>` + stringsNavRoutes + block + `</head><body>` +
		`<div data-cui-layout="site" data-cui-layout-key="` + key + `" data-cui-lang="` + lang + `">` + outlet +
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
	shell := func(key, lang, strs, id, next string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", id)
				w.Header().Set("X-Gofastr-Swap", key)
				fmt.Fprint(w, stringsNavPartial(lang, strs, id, next))
				return
			}
			fmt.Fprint(w, stringsNavPage(lang, key, strs, id, next))
		}
	}
	mux.HandleFunc("/s/en", shell("l:site", "en", "", "s-en", "/s/es"))
	mux.HandleFunc("/s/es", shell("l:site", "es", stringsES, "s-es", "/s/en"))
	mux.HandleFunc("/e/en", shell("l:site", "en", "", "e-en", "/e/es"))
	mux.HandleFunc("/e/es", shell("l:site", "es", stringsES, "e-es", "/e/en"))
	mux.HandleFunc("/k/en", shell("l:k-en", "en", "", "k-en", "/k/es"))
	mux.HandleFunc("/k/es", shell("l:k-es", "es", stringsES, "k-es", "/k/en2"))
	mux.HandleFunc("/k/en2", shell("l:k-es", "en", "", "k-en", "/k/es"))
	mux.HandleFunc("/f/en", shell("l:f-en", "en", "", "f-en", "/f/es"))
	mux.HandleFunc("/f/es", shell("l:f-es", "es", stringsES, "f-es", "/f/en2"))
	mux.HandleFunc("/f/en2", shell("l:f-es", "en", "", "f-en", "/f/es"))
	mux.HandleFunc("/g/en", shell("l:g-en", "en", "", "g-en", "/g/es"))
	mux.HandleFunc("/g/es", shell("l:g-es", "es", stringsES, "g-es", "/g/en"))
	mux.HandleFunc("/g/en2", shell("l:g-es", "en", "", "g-en2", "/g/es"))
	mux.HandleFunc("/h/en", shell("l:h-en", "en", "", "h-en", "/h/es"))
	mux.HandleFunc("/h/es", shell("l:h-es", "es", stringsES, "h-es", "/h/en"))
	mux.HandleFunc("/h/en2", shell("l:h-es", "en", "", "h-en2", "/h/es"))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const probe = `__gofastr.say('runtime.load_failed', 'Could not load {{path}}', {path: '/x'})`
	for _, base := range []string{"/s", "/k", "/e", "/f"} {
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		var en1, es, en2, esBack, errEN, errES string
		id := base[1:]
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+base+"/en"),
			chromedp.WaitVisible(`#`+id+`-en`, chromedp.ByID),
			chromedp.Evaluate(probe, &en1),
			chromedp.Evaluate(rpcErrProbe, &errEN),
			chromedp.Click(`#go-`+id+`-en`, chromedp.ByID),
			chromedp.WaitVisible(`#`+id+`-es`, chromedp.ByID),
			chromedp.Evaluate(probe, &es),
			chromedp.Evaluate(rpcErrProbe, &errES),
			chromedp.Click(`#go-`+id+`-es`, chromedp.ByID),
			chromedp.WaitVisible(`#`+id+`-en`, chromedp.ByID),
			chromedp.Evaluate(probe, &en2),
			chromedp.NavigateBack(),
			chromedp.WaitVisible(`#`+id+`-es`, chromedp.ByID),
			chromedp.Evaluate(probe, &esBack),
		); err != nil {
			t.Fatalf("%s: chromedp: %v", base, err)
		}
		if en1 != "Could not load /x" || es != "No se pudo cargar /x" || en2 != "Could not load /x" || esBack != es {
			t.Errorf("%s: say across en → es → en → back = %q / %q / %q / %q", base, en1, es, en2, esBack)
		}
		if errEN != "Request failed (500) \u2014 boom" || errES != "Error en la solicitud (500) \u2014 boom" {
			t.Errorf("%s: failed-RPC signal text en / es = %q / %q", base, errEN, errES)
		}
	}
	for _, base := range []string{"/g", "/h"} {
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		id := base[1:]
		step := func(path, want string, out *string) chromedp.Action {
			return chromedp.Tasks{
				chromedp.Evaluate(`__gofastr.navigate('`+base+path+`')`, nil),
				chromedp.WaitVisible(`#`+id+want, chromedp.ByID),
				chromedp.Evaluate(probe, out),
			}
		}
		var en1, es, enMid, en2, esAgain string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+base+"/en"),
			chromedp.WaitVisible(`#`+id+`-en`, chromedp.ByID),
			chromedp.Evaluate(probe, &en1),
			step("/es", "-es", &es),
			step("/en", "-en", &enMid),
			step("/en2", "-en2", &en2),
			step("/es", "-es", &esAgain),
		); err != nil {
			t.Fatalf("%s: chromedp: %v", base, err)
		}
		if es != "No se pudo cargar /x" || en2 != "Could not load /x" || esAgain != es {
			t.Errorf("%s: say across en → es → en → en2 → es = %q / %q / %q / %q / %q", base, en1, es, enMid, en2, esAgain)
		}
	}
}
