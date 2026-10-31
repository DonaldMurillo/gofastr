package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A stream whose caller the re-check places in another tenant closes, as
// the CRUD event stream does. The re-check re-resolved the tenant
// (tenant.ResolveMiddleware installs that step), but delivery only ran
// the gates, and a gate that does not read the tenant kept sending the
// old tenant's resource URIs and list_changed notices.
func TestTenantChangeClosesStream(t *testing.T) {
	var current atomic.Value
	current.Store("tenant-a")
	s := NewServer()
	h := s.ServeSSE("/mcp")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.SetUser(r.Context(), revocableUser{})
		ctx = handler.SetTenantID(ctx, current.Load().(string))
		ctx = handler.WithPrincipalCheck(ctx, func(ctx context.Context) (context.Context, bool) {
			return handler.SetTenantID(ctx, current.Load().(string)), true
		})
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(ts.Close)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	s.NotifyToolsListChanged()
	if ev := awaitSSE(t, events, "list_changed in the connect-time tenant"); ev.event != "message" {
		t.Fatalf("event = %q, want message", ev.event)
	}
	current.Store("tenant-b")
	s.NotifyToolsListChanged()
	select {
	case ev, ok := <-events:
		if ok {
			t.Fatalf("SECURITY: [tenant] a caller moved to another tenant still received %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream of a caller moved to another tenant stayed open")
	}
}
