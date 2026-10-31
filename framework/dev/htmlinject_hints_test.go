package dev

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A 103 Early Hints decided the response before the page's own header:
// the 201 was dropped and the script never spliced.
func TestInjectorKeepsPageAfterHints(t *testing.T) {
	srv := httptest.NewServer(LiveReloadHTMLInjector()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</app.css>; rel=preload; as=style")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "<html><body>made</body></html>")
	})))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if !strings.Contains(string(body), LiveReloadScriptURL) {
		t.Fatalf("script not spliced after hints:\n%s", body)
	}
}
