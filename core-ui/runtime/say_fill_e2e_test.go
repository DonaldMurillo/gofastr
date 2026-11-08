package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

var (
	sayFillCanonical = i18n.Define("runtimetest.say_canonical", "Hello, {{name}}!")
	sayFillSpaced    = i18n.Define("runtimetest.say_spaced", "Hello, {{ name }}!")
	sayFillHyphen    = i18n.Define("runtimetest.say_hyphen", "Hello, {{user-name}}!")
	sayFillEnglish   = i18n.Define("runtimetest.say_english", "Bye, {{ user-name }}.")
)

// A phrase the catalog check accepts must read the same from the
// browser's say() as from Phrase.Text: i18n.Fill trims the name and
// takes any text up to }}, while say() took only \w+ names, so
// {{ name }} and {{user-name}} stayed literal after a client update.
// The last phrase has no translation: say() fills its English.
func TestRuntimeSayFillsLikeGo(t *testing.T) {
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", sayFillCanonical.Key(), i18n.Message{Text: "Bonjour, {{name}} !"})
	catalog.Set("fr", sayFillSpaced.Key(), i18n.Message{Text: "Bonjour, {{ name }} !"})
	catalog.Set("fr", sayFillHyphen.Key(), i18n.Message{Text: "Bonjour, {{user-name}} !"})
	if issues := i18n.CheckCatalog(catalog, "fr"); len(issues) != 0 {
		t.Fatalf("catalog check = %+v, want none", issues)
	}
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(catalog, "en"))
	phrases := []i18n.Phrase{sayFillCanonical, sayFillSpaced, sayFillHyphen, sayFillEnglish}
	RegisterStrings(phrases...)
	params := map[string]any{"name": "Ada", "user-name": "Bo"}
	var want []string
	var calls []map[string]string
	for _, phrase := range phrases {
		want = append(want, phrase.Text(ctx, params))
		calls = append(calls, map[string]string{"key": phrase.Key(), "en": phrase.English()})
	}
	callJSON, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	paramJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}

	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="fr"><head>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>`+
			`<script type="application/json" id="gofastr-strings">`+string(StringsJSON(ctx))+`</script>`+
			`</head><body><main role="main" tabindex="-1" id="m">m</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	browser := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got []string
	if err := chromedp.Run(browser,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#m`, chromedp.ByID),
		chromedp.Evaluate(`(`+string(callJSON)+`).map((call) => __gofastr.say(call.key, call.en, `+string(paramJSON)+`))`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("say() answered %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("%s: say() = %q, Phrase.Text = %q", phrases[index].Key(), got[index], want[index])
		}
	}
}
