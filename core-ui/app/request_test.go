package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestFromContextReturnsNilWhenUnset(t *testing.T) {
	if r := RequestFromContext(context.Background()); r != nil {
		t.Errorf("expected nil request, got %v", r)
	}
}

func TestWithRequestStoresAndRetrieves(t *testing.T) {
	req := httptest.NewRequest("GET", "/x?sort=email&dir=desc", nil)
	ctx := WithRequest(context.Background(), req)

	got := RequestFromContext(ctx)
	if got == nil {
		t.Fatal("expected request, got nil")
	}
	if got.URL.Path != "/x" {
		t.Errorf("path = %q, want /x", got.URL.Path)
	}
}

func TestQueryFromContextReadsValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/x?sort=email&dir=desc&p=3", nil)
	ctx := WithRequest(context.Background(), req)

	q := QueryFromContext(ctx)
	if got := q.Get("sort"); got != "email" {
		t.Errorf("sort = %q, want email", got)
	}
	if got := q.Get("dir"); got != "desc" {
		t.Errorf("dir = %q, want desc", got)
	}
	if got := q.Get("p"); got != "3" {
		t.Errorf("p = %q, want 3", got)
	}
}

func TestQueryFromContextEmptyWhenNoRequest(t *testing.T) {
	q := QueryFromContext(context.Background())
	if len(q) != 0 {
		t.Errorf("expected empty Values, got %v", q)
	}
}

func TestWithRequestNilIsNoop(t *testing.T) {
	ctx := WithRequest(context.Background(), nil)
	if r := RequestFromContext(ctx); r != nil {
		t.Errorf("expected nil from nil request, got %v", r)
	}
}

// Sanity: standard library's *http.Request roundtrips through context
// with all fields intact, including custom headers a screen might
// want to inspect.
func TestWithRequestPreservesHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Something", "value")
	ctx := WithRequest(context.Background(), req)

	got := RequestFromContext(ctx)
	if got.Header.Get("X-Something") != "value" {
		t.Errorf("header lost; got %q", got.Header.Get("X-Something"))
	}
	if got.Method != http.MethodGet {
		t.Errorf("method = %q", got.Method)
	}
}
