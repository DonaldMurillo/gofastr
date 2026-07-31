package builtins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization header leaked through: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Harness-Token") != "" {
			t.Errorf("X-Harness-Token header leaked through")
		}
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	tool := WebFetch{HTTPClient: srv.Client(), AllowPrivateHosts: true}
	res, _ := tool.Run(context.Background(), mustCall(t, map[string]any{
		"url": srv.URL,
		"headers": map[string]string{
			"Authorization":   "Bearer should-be-stripped",
			"X-Harness-Token": "tok_should-be-stripped",
			"User-Agent":      "harness-test",
		},
	}), nil)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "hello world") {
		t.Errorf("body missing: %q", res.Content[0].Text)
	}
}

func TestWebFetchRejectsNonHTTP(t *testing.T) {
	res, _ := (WebFetch{}).Run(context.Background(), mustCall(t, map[string]any{
		"url": "file:///etc/passwd",
	}), nil)
	if !res.IsError {
		t.Fatal("expected error for file:// URL")
	}
}

func TestWebFetch404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("missing"))
	}))
	defer srv.Close()

	tool := WebFetch{HTTPClient: srv.Client(), AllowPrivateHosts: true}
	res, _ := tool.Run(context.Background(), mustCall(t, map[string]any{"url": srv.URL}), nil)
	if !res.IsError {
		t.Fatal("expected IsError on 4xx")
	}
}
