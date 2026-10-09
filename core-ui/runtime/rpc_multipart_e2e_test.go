package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// An RPC form that holds a file input posts multipart. Its body keeps
// the JSON body's shape: a bool's hidden-false + checkbox pair is one
// value (the last), and a file input left empty sends no part, so a
// save that picks no new file keeps the stored one.
func TestRPCMultipartFoldsPairsAndDropsEmptyFiles(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got map[string][]string
	var files []string
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mu.Lock()
		got = r.MultipartForm.Value
		for k := range r.MultipartForm.File {
			files = append(files, k)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body>
<form id="f" action="/save" method="POST" data-cui-rpc="/save" data-cui-rpc-method="PUT">
  <input type="text" name="title" value="Hello">
  <input type="hidden" name="active" value="false"><input type="checkbox" name="active" value="true" checked>
  <input type="file" name="logo">
  <button id="go" type="submit">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	var ok bool
	for range 100 {
		mu.Lock()
		ok = got != nil
		mu.Unlock()
		if ok {
			break
		}
		_ = chromedp.Run(ctx, chromedp.Sleep(50e6))
	}
	mu.Lock()
	defer mu.Unlock()
	if !ok {
		t.Fatal("the form never posted")
	}
	if v := got["active"]; len(v) != 1 || v[0] != "true" {
		t.Errorf("active = %q, want one value \"true\"", v)
	}
	if v := got["title"]; len(v) != 1 || v[0] != "Hello" {
		t.Errorf("title = %q", v)
	}
	if len(files) != 0 || len(got["logo"]) != 0 {
		t.Errorf("an empty file input sent a part: files %v, values %v", files, got["logo"])
	}
}
