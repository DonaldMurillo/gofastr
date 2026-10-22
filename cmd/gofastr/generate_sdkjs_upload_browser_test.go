package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestJSSDKUploadGuards runs the generated client.js in Chrome against a
// stub that records what arrives. A write the server would refuse throws
// before it is sent; a good one goes as multipart with parts by column.
func TestJSSDKUploadGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	spec, err := buildSDKSpec([]framework.EntityDeclaration{uploadDecl()}, &sdkOptions{name: "uploads", sdkVersion: "0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKFiles(spec, []string{"js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var clientJS, clientDTS string
	for _, f := range files {
		switch f.name {
		case "js/client.js":
			clientJS = f.content
		case "js/client.d.ts":
			clientDTS = f.content
		}
	}
	// A batch goes as JSON, so its item types take no Blob.
	for _, want := range []string{
		"batchCreate(items: Array<NoBlob<TInput>>)",
		"batchUpdate(items: Array<NoBlob<TPatch> & TItemKey>)",
	} {
		if !strings.Contains(clientDTS, want) {
			t.Errorf("client.d.ts missing %q", want)
		}
	}

	var mu sync.Mutex
	hits := 0
	parts := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/harness", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<!doctype html><html><body>harness</body></html>")
	})
	mux.HandleFunc("/customers", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hits++
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			body, _ := io.ReadAll(p)
			parts[p.FormName()] = string(body)
			if len(body) > 64 {
				parts[p.FormName()] = fmt.Sprintf("<%d bytes>", len(body))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"id":"c1","name":"Acme"}}`)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	scenario := fmt.Sprintf(`(async () => {
  const { Client } = await import('/client.js');
  const api = new Client({ baseURL: location.origin });
  const max = %d;
  const body = %d;
  const blob = (n) => new File([new Uint8Array(n)], 'logo.png');
  const refused = (p) => p.then(() => 'sent', (e) => String(e.message));
  const r = {
    tooBig: await refused(api.customers.create({ name: 'a', logo: blob(max + 1) })),
    // The files alone fit by 100 bytes; the part framing does not.
    bodyTooBig: await refused(api.customers.create({ name: 'a', logo: blob(max), contract: blob(body - max - 100) })),
    nullValue: await refused(api.customers.create({ name: null, logo: blob(3) })),
    unknown: await refused(api.customers.create({ nope: 1, logo: blob(3) })),
    batch: await refused(api.customers.batchCreate([{ name: 'a', logo: blob(3) }])),
  };
  const created = await api.customers.create({ name: 'Acme', planTier: 'pro', logo: new File(['PNG'], 'a.png') });
  r.createdID = created.id;
  window.__result = r;
})().catch((e) => { window.__result = { fatal: String((e && e.stack) || e) }; });
true`, file.MaxProcessFileSize, crud.MaxMultipartBodyBytes)

	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	var r map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/harness"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(scenario, nil),
		chromedp.Poll(`window.__result`, &r, chromedp.WithPollingTimeout(60*time.Second)),
	); err != nil {
		t.Fatalf("browser: %v", err)
	}
	if fatal, ok := r["fatal"]; ok {
		t.Fatalf("scenario failed in the browser: %v", fatal)
	}
	for key, want := range map[string]string{
		"tooBig":     "over the server's limit of 32 MiB per file",
		"bodyTooBig": "request is over the server's limit of 64 MiB",
		"nullValue":  "is null",
		"unknown":    "has no column",
		"batch":      "a batch sends JSON",
	} {
		if got, _ := r[key].(string); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("the server saw %d writes, want only the good one", hits)
	}
	if r["createdID"] != "c1" {
		t.Errorf("create returned %v", r["createdID"])
	}
	want := map[string]string{"name": "Acme", "plan_tier": "pro", "logo": "PNG"}
	if len(parts) != len(want) {
		t.Errorf("parts = %v, want %v", parts, want)
	}
	for k, v := range want {
		if parts[k] != v {
			t.Errorf("part %s = %q, want %q", k, parts[k], v)
		}
	}
}
