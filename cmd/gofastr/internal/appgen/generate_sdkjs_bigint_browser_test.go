package appgen

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Issue #614: the JS client parsed with JSON.parse, so an integer past
// 2^53 (a snowflake id, a big counter) lost digits, the bug floatnum
// closed on the Go side. Such an integer now arrives as its exact decimal
// string; every other value parses as before.
func TestJSSDKKeepsBigIntegers(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	decls := []framework.EntityDeclaration{{
		Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "views", Type: "int"}},
	}}
	spec, err := buildSDKSpec(decls, &SdkOptions{name: "bigint", sdkVersion: "0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKFiles(spec, []string{"js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var clientJS string
	for _, f := range files {
		if f.Name == "js/client.js" {
			clientJS = f.Content
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>harness</title><body>harness</body>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const body = `{"id":9007199254740993,"views":42,"neg":-9007199254740993,"edge":9007199254740991,` +
		`"f":1.5,"e":1e21,"z":-0,"big":12345678901234567890,` +
		`"s":"x 9007199254740993 \"q\\\" 1","t":"a\"9007199254740993","arr":[9007199254740993, 1,{"k":18446744073709551615}]}`
	ctx := chromedptest.Context(t)
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`(async () => {
			const { Client } = await import('/client.js');
			window.fetch = async () => new Response(`+fmt.Sprintf("%q", body)+`, { status: 200, headers: { 'Content-Type': 'application/json' } });
			const api = new Client({ baseURL: location.origin });
			const r = await api.posts.get('1');
			window.__result = JSON.stringify([
				r.id, r.views, r.neg, r.edge, r.f, r.e, Object.is(r.z, -0), r.big, r.s, r.t, r.arr[0], r.arr[1], r.arr[2].k,
			].map((v) => typeof v + ':' + String(v)));
		})().catch((e) => { window.__result = 'fatal: ' + String(e); }); true`, nil),
		chromedp.Poll(`!!window.__result`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__result`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	want := `["string:9007199254740993","number:42","string:-9007199254740993","number:9007199254740991",` +
		`"number:1.5","number:1e+21","boolean:true","string:12345678901234567890",` +
		`"string:x 9007199254740993 \"q\\\" 1","string:a\"9007199254740993","string:9007199254740993","number:1","string:18446744073709551615"]`
	if got != want {
		t.Fatalf("parsed values\n got %s\nwant %s", got, want)
	}
}
