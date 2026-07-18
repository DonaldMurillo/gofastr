package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestGeneratedJSSDK_BrowserRoundTrip executes the generated client.js in a
// real Chrome against a live framework server on the same origin — both the
// syntax check (Node is not in CI; the browser is the repo-standard JS
// runtime) and the behavioral contract: camelCase responses, snake_case
// filter params and validation-error keys, presence-faithful PATCH ({views:0}
// must set zero), batch rollback surfaced as a result, and the SSE watch.
func TestGeneratedJSSDK_BrowserRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}

	decls := []framework.EntityDeclaration{{
		Name:  "posts",
		Table: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "author_name", Type: "string", Required: true},
			{Name: "views", Type: "int"},
			{Name: "published", Type: "bool"},
		},
	}}
	opts := sdkOptions{name: "browser", sdkVersion: "0.0.1"}
	spec, err := buildSDKSpec(decls, &opts)
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKFiles(spec, []string{"js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var clientJS string
	for _, f := range files {
		if f.name == "js/client.js" {
			clientJS = f.content
		}
	}
	if clientJS == "" {
		t.Fatal("client.js not rendered")
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithoutDefaultMiddleware(),
	)
	app.Entity("posts", framework.EntityConfig{
		Table:  "posts",
		Public: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_name", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "published", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/harness", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><body>harness</body></html>")
	})
	mux.Handle("/", app.Router())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	ctx, cancel := context.WithTimeout(browserCtx, 120*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/harness"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	scenario := `(async () => {
  const { Client, ApiError, postsFields } = await import('/client.js');
  const api = new Client({ baseURL: location.origin });
  const r = {};

  const created = await api.posts.create({ title: 'hello', authorName: 'ada', views: 5 });
  r.createdTitle = created.title;
  r.createdAuthor = created.authorName; // camelCase response key
  await api.posts.create({ title: 'low', authorName: 'bob', views: 1 });

  const filtered = await api.posts.list({ sort: '-views', filters: { [postsFields.views + '_gte']: 3 } });
  r.filteredTotal = filtered.total;

  const patched = await api.posts.patch(created.id, { views: 0 });
  r.patchedViews = patched.views;
  r.patchedTitle = patched.title; // untouched field must survive

  try {
    await api.posts.create({ title: 'no author' });
    r.valFields = 'no-error';
  } catch (e) {
    r.valIsApiError = e instanceof ApiError;
    r.valStatus = e.status;
    r.valFields = e.fields ? Object.keys(e.fields).join(',') : 'none';
  }

  const rb = await api.posts.batchCreate([{ title: 'ok', authorName: 'c' }, {}]);
  r.rbCommitted = rb.committed;
  r.rbLen = rb.results.length;

  let resolveEvt;
  const evtP = new Promise((res) => { resolveEvt = res; });
  const ctrl = new AbortController();
  api.posts.watch((event) => resolveEvt(event), { signal: ctrl.signal }).catch(() => {});
  let evt = null;
  for (let i = 0; i < 40 && !evt; i++) {
    await api.posts.create({ title: 'sse' + i, authorName: 'w' });
    evt = await Promise.race([evtP, new Promise((res) => setTimeout(() => res(null), 250))]);
  }
  ctrl.abort();
  r.watchEvent = evt;

  await api.posts.remove(created.id);
  r.getAfterDelete = await api.posts.get(created.id).then(() => 'found', (e) => e.status);
  window.__result = r;
})().catch((e) => { window.__result = { fatal: String((e && e.stack) || e) }; });
true`

	if err := chromedp.Run(ctx, chromedp.Evaluate(scenario, nil)); err != nil {
		t.Fatalf("start scenario: %v", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var done bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__result`, &done)); err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scenario did not finish")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var r map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__result`, &r)); err != nil {
		t.Fatal(err)
	}
	if fatal, ok := r["fatal"]; ok {
		t.Fatalf("scenario failed in browser: %v", fatal)
	}

	if r["createdTitle"] != "hello" || r["createdAuthor"] != "ada" {
		t.Errorf("create/camelCase response broken: %+v", r)
	}
	if r["filteredTotal"] != float64(1) {
		t.Errorf("snake_case filter param broken: filteredTotal=%v", r["filteredTotal"])
	}
	if r["patchedViews"] != float64(0) || r["patchedTitle"] != "hello" {
		t.Errorf("presence-faithful patch broken: %+v", r)
	}
	if r["valIsApiError"] != true || r["valStatus"] != float64(400) {
		t.Errorf("validation error shape broken: %+v", r)
	}
	if fields, _ := r["valFields"].(string); !strings.Contains(fields, "author_name") {
		t.Errorf("validation fields must use snake_case keys, got %v", r["valFields"])
	}
	if r["rbCommitted"] != false || r["rbLen"] != float64(2) {
		t.Errorf("batch rollback must surface as an uncommitted result: %+v", r)
	}
	if r["watchEvent"] != "entity.created" {
		t.Errorf("watch/SSE broken: %v", r["watchEvent"])
	}
	if r["getAfterDelete"] != float64(404) {
		t.Errorf("delete/get-after broken: %v", r["getAfterDelete"])
	}
}
