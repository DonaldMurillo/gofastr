package appgen

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

const freshnessSpec = `app:
  name: Freshness
  module: example.com/freshness
  db:
    driver: sqlite
    url: file:freshness.db
entities:
  - name: notes
    table: memos
    crud: true
    public: true
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: home
    route: /
    title: Notes
    body:
      - type: link
        text: New note
        href: /notes/new
      - type: entity_list
        entity: notes
        fields: [title]
  - name: mirror
    route: /mirror
    body:
      - kind: section
        children:
          - kind: entity_list
            entity: notes
            fields: [title]
  - name: note_new
    route: /notes/new
    body:
      - kind: entity_form
        entity: notes
`

func freshnessAppSpec(t *testing.T) appspec.AppSpec {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, freshnessSpec)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	return bp
}

func TestGeneratedEntityFormInvalidatesScreens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a generated app in Chrome")
	}
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	version, err := clitest.RepoGoVersion(repo)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/freshness\n\ngo "+version+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\nreplace github.com/DonaldMurillo/gofastr => "+repo+"\n")
	if err := clitest.CopyGoSum(repo, dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range mustRenderAppSpecFiles(t, freshnessAppSpec(t)) {
		clitest.WriteFileAll(t, filepath.Join(dir, f.Name), f.Content)
	}
	bin := filepath.Join(dir, "app")
	build := exec.Command("go", "build", "-mod=mod", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generated app: %v\n%s", err, out)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	server := exec.Command(bin)
	server.Dir = dir
	server.Env = append(os.Environ(), "PORT="+addr, "DATABASE_URL=file:"+filepath.Join(dir, "test.db"))
	log, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _, _ = server.Process.Wait() })
	base := "http://" + addr
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	client := &http.Client{Timeout: time.Second}
	ready := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		r, err := client.Get(base + "/")
		if err == nil {
			r.Body.Close()
			ready = r.StatusCode == http.StatusOK
			if ready {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		out, _ := os.ReadFile(filepath.Join(dir, "server.log"))
		t.Fatalf("app not ready: %s", out)
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), chromedp.Poll(`!!window.__gofastr?.navigate`, nil), chromedp.Evaluate(`window.__freshnessDocument = true`, nil)); err != nil {
		t.Fatal(err)
	}
	var evictions int
	if err := chromedp.Run(ctx,
		chromedp.Click(`a[href="/notes/new"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			window.__freshnessEvictions = [];
			const invalidate = __gofastr.invalidate.bind(__gofastr);
			__gofastr.invalidate = (...paths) => { __freshnessEvictions.push(paths); invalidate(...paths); };
			const fetch = __gofastr.fetch.bind(__gofastr);
			__gofastr.fetch = (...args) => fetch(...args).then(r => { if (r.status >= 400) window.__freshnessRejected = r.status; return r; });
			document.querySelector('form[data-entity-form="notes"]').noValidate = true;
			document.querySelector('input[name="title"]').removeAttribute('required');
		})()`, nil),
		chromedp.Click(`form[data-entity-form="notes"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.Poll(`window.__freshnessRejected >= 400 && document.querySelector('form[data-entity-form="notes"]').getAttribute('aria-busy') !== 'true'`, nil),
		chromedp.Evaluate(`__freshnessEvictions.length`, &evictions),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`a[href="/notes/new"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if evictions != 0 {
		t.Fatalf("rejected create evicted %d cache entries", evictions)
	}
	for i := 1; i <= 2; i++ {
		name := fmt.Sprintf("fresh-row-%d", i)
		err := chromedp.Run(ctx,
			chromedp.Evaluate(`__gofastr.navigate('/mirror')`, nil),
			chromedp.Poll(`location.pathname === '/mirror' && !document.documentElement.hasAttribute('aria-busy')`, nil),
			chromedp.Evaluate(`__gofastr.navigate('/')`, nil),
			chromedp.WaitVisible(`a[href="/notes/new"]`, chromedp.ByQuery),
			chromedp.Click(`a[href="/notes/new"]`, chromedp.ByQuery),
			chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
			chromedp.SendKeys(`input[name="title"]`, name, chromedp.ByQuery),
			chromedp.Click(`form[data-entity-form="notes"] button[type="submit"]`, chromedp.ByQuery),
			chromedp.Poll(`document.querySelector('input[name="title"]')?.value === '' && !document.querySelector('form[aria-busy="true"]')`, nil),
			chromedp.Evaluate(`history.back()`, nil),
			chromedp.WaitVisible(`a[href="/notes/new"]`, chromedp.ByQuery),
		)
		if err != nil {
			t.Fatal(err)
		}
		var text string
		if err := chromedp.Run(ctx, chromedp.Text("main", &text)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, name) {
			t.Fatalf("home after authored create shows stale rows: missing %q in %q", name, text)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`__gofastr.navigate('/mirror')`, nil), chromedp.Poll(`location.pathname === '/mirror' && document.querySelector('main').textContent.includes(`+fmt.Sprintf("%q", name)+`)`, nil)); err != nil {
			t.Fatalf("second cached screen stayed stale: %v", err)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`__gofastr.navigate('/')`, nil), chromedp.WaitVisible(`a[href="/notes/new"]`, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	var sameDocument bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__freshnessDocument === true`, &sameDocument)); err != nil || !sameDocument {
		t.Fatalf("create caused a full reload: sameDocument=%v, err=%v", sameDocument, err)
	}
}
