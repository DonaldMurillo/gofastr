package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// The re-dispatched request comes from the MCP caller's address, with the
// proxy headers a trusted-proxy limiter reads. It came from a fixed
// placeholder, so every MCP caller shared one per-IP rate-limit bucket
// and one caller could spend everyone's.
func TestRedispatchKeepsCallerPeer(t *testing.T) {
	var got *http.Request
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusNoContent)
	})
	orig := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	orig.RemoteAddr = "203.0.113.7:5555"
	orig.Header.Add("X-Forwarded-For", "198.51.100.1")
	orig.Header.Add("X-Forwarded-For", "198.51.100.2")
	orig.Header.Set("X-Real-IP", "198.51.100.3")
	orig.Header.Set("X-Other", "dropped")
	ctx := mcp.WithRequest(context.Background(), orig)

	if _, err := runToolRequest(ctx, router, http.MethodGet, "/api/notes", nil); err != nil {
		t.Fatalf("runToolRequest: %v", err)
	}
	if got.RemoteAddr != orig.RemoteAddr {
		t.Errorf("RemoteAddr = %q, want the caller's %q", got.RemoteAddr, orig.RemoteAddr)
	}
	if xff := got.Header.Values("X-Forwarded-For"); len(xff) != 2 || xff[0] != "198.51.100.1" || xff[1] != "198.51.100.2" {
		t.Errorf("X-Forwarded-For = %q, want both of the caller's fields", xff)
	}
	if v := got.Header.Get("X-Real-IP"); v != "198.51.100.3" {
		t.Errorf("X-Real-IP = %q, want the caller's", v)
	}
	if v := got.Header.Get("X-Other"); v != "" {
		t.Errorf("X-Other = %q, want only credential and peer headers copied", v)
	}
}

// The re-dispatched request names the host the MCP caller reached. It
// carried httptest's example.com, so host middleware (a tenant or site
// picked from the host, a custom-domain router) served every generated
// tool as some other site (2026-10-27 bug bash P15).
func TestRedispatchKeepsCallerHost(t *testing.T) {
	var got *http.Request
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusNoContent)
	})
	orig := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	orig.Host = "acme.app.test"
	ctx := mcp.WithRequest(context.Background(), orig)

	if _, err := runToolRequest(ctx, router, http.MethodGet, "/api/notes", nil); err != nil {
		t.Fatalf("runToolRequest: %v", err)
	}
	if got.Host != orig.Host {
		t.Errorf("Host = %q, want the caller's %q", got.Host, orig.Host)
	}
}
