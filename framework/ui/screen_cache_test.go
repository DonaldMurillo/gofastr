package ui

import (
	"net/http/httptest"
	"testing"
)

func TestInvalidateScreensHeader(t *testing.T) {
	w := httptest.NewRecorder()
	InvalidateScreens(w, "/orders")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != `["/orders"]` {
		t.Fatalf("header = %q, want %q", got, `["/orders"]`)
	}
}

func TestInvalidateScreensAccumulates(t *testing.T) {
	w := httptest.NewRecorder()
	InvalidateScreens(w, "/orders")
	InvalidateScreens(w, "/dashboard?range=7d", "/reports")
	want := `["/orders","/dashboard?range=7d","/reports"]`
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestInvalidateScreensCommaInQuery(t *testing.T) {
	// Commas are legal in query values — the JSON array must carry them
	// intact (this is why the header is not comma-separated).
	w := httptest.NewRecorder()
	InvalidateScreens(w, "/search?q=red,blue")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != `["/search?q=red,blue"]` {
		t.Fatalf("header = %q", got)
	}
}

func TestInvalidateScreensWildcard(t *testing.T) {
	w := httptest.NewRecorder()
	InvalidateScreens(w, "*")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != `["*"]` {
		t.Fatalf("header = %q, want %q", got, `["*"]`)
	}
}

func TestInvalidateScreensDropsInvalid(t *testing.T) {
	// Only root-relative paths and "*" are meaningful cache keys; the
	// rest are silently dropped so a bad value can't smuggle anything
	// odd into the header.
	w := httptest.NewRecorder()
	InvalidateScreens(w, "", "https://evil.example/x", "//proto-relative", "orders",
		"/del\x7f", "/nl\n", "/tab\tx", "*", "/ok")
	want := `["*","/ok"]`
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestInvalidateScreensNoValidPathsNoHeader(t *testing.T) {
	w := httptest.NewRecorder()
	InvalidateScreens(w)
	InvalidateScreens(w, "", "relative", "https://x")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != "" {
		t.Fatalf("header should be unset, got %q", got)
	}
}

func TestInvalidateScreensReplacesMalformedExisting(t *testing.T) {
	// A manually written malformed value must not poison accumulation —
	// the helper replaces it with a valid list.
	w := httptest.NewRecorder()
	w.Header().Set("X-Gofastr-Invalidate", "not-json")
	InvalidateScreens(w, "/orders")
	if got := w.Header().Get("X-Gofastr-Invalidate"); got != `["/orders"]` {
		t.Fatalf("header = %q, want %q", got, `["/orders"]`)
	}
}
