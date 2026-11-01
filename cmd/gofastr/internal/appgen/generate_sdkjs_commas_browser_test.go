package appgen

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The generated client refuses a list element holding a comma. It sends
// a filter list as one comma-joined value, and the server splits _in
// values on commas, so title_in: ["Doe, Jane"] asked for "Doe" and
// " Jane" (2026-10-27 bug bash D30). A list without commas still goes
// as one joined value.
func TestJSSDKRefusesCommaInList(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	decls := []framework.EntityDeclaration{{
		Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}}
	spec, err := buildSDKSpec(decls, &SdkOptions{name: "commas", sdkVersion: "0.0.1"})
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

	ctx := chromedptest.Context(t)
	var got struct {
		Joined  string `json:"joined"`
		Refused string `json:"refused"`
		Sent    int    `json:"sent"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`(async () => {
			const { Client } = await import('/client.js');
			const r = { joined: '', refused: '', sent: 0 };
			window.fetch = async (url) => {
				r.sent++;
				r.joined = new URL(String(url)).searchParams.get('title_in') || '';
				return new Response('{"data":[],"total":0}', { status: 200, headers: { 'Content-Type': 'application/json' } });
			};
			const api = new Client({ baseURL: location.origin });
			await api.posts.list({ filters: { title_in: ['Doe', 'Jane'] } });
			try {
				await api.posts.list({ filters: { title_in: ['Doe, Jane'] } });
			} catch (e) {
				r.refused = String(e.message);
			}
			window.__result = r;
		})().catch((e) => { window.__result = { refused: 'fatal: ' + String(e) }; }); true`, nil),
		chromedp.Poll(`!!window.__result`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__result`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got.Joined != "Doe,Jane" {
		t.Errorf("title_in went as %q, want Doe,Jane", got.Joined)
	}
	if !strings.Contains(got.Refused, "comma") || strings.HasPrefix(got.Refused, "fatal: ") {
		t.Errorf("a list element holding a comma was sent (%q)", got.Refused)
	}
	if got.Sent != 1 {
		t.Errorf("%d requests went out, want 1: the refused list must not be sent", got.Sent)
	}
}

// The generated client refuses an array on a filter key that is not an
// _in key. The server reads a bare key as equality with the whole value,
// so status: ["a", "b"] went as ?status=a,b and matched rows whose status
// is literally "a,b". The top-level lists (sort here) still go
// comma-joined.
func TestJSSDKRefusesArrayOnBareKey(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	decls := []framework.EntityDeclaration{{
		Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}}
	spec, err := buildSDKSpec(decls, &SdkOptions{name: "barekey", sdkVersion: "0.0.1"})
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

	ctx := chromedptest.Context(t)
	var got struct {
		Query   string `json:"query"`
		Refused string `json:"refused"`
		Sent    int    `json:"sent"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`(async () => {
			const { Client } = await import('/client.js');
			const r = { query: '', refused: '', sent: 0 };
			window.fetch = async (url) => {
				r.sent++;
				r.query = decodeURIComponent(new URL(String(url)).search);
				return new Response('{"data":[],"total":0}', { status: 200, headers: { 'Content-Type': 'application/json' } });
			};
			const api = new Client({ baseURL: location.origin });
			await api.posts.list({ sort: ['title', '-id'], filters: { title_in: ['a', 'b'] } });
			try {
				await api.posts.list({ filters: { title: ['a', 'b'] } });
			} catch (e) {
				r.refused = String(e.message);
			}
			window.__result = r;
		})().catch((e) => { window.__result = { refused: 'fatal: ' + String(e) }; }); true`, nil),
		chromedp.Poll(`!!window.__result`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__result`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got.Query != "?sort=title,-id&title_in=a,b" {
		t.Errorf("query went as %q, want ?sort=title,-id&title_in=a,b", got.Query)
	}
	if !strings.Contains(got.Refused, "title_in") || strings.HasPrefix(got.Refused, "fatal: ") {
		t.Errorf("an array on the bare title key was not refused naming title_in (%q)", got.Refused)
	}
	if got.Sent != 1 {
		t.Errorf("%d requests went out, want 1: the refused list must not be sent", got.Sent)
	}
}
