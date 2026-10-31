package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// A MultiTenant event stream filtered on the tenant it read at connect.
// A principal moved to another tenant after connect kept receiving the old
// tenant's writes until it disconnected (2026-10-27 bug bash, unrechecked):
// the re-check refreshed the identity but never compared the tenant.
func TestEventStreamClosesOnTenantChange(t *testing.T) {
	ch, _ := setupTenantInProcHandler(t)
	ch.Events = event.NewEventBus()
	ch.WithEventStreamReauth(time.Second)
	var moved atomic.Bool
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1")
	ctx := tenant.SetTenantID(req.Context(), "tenant-a")
	ctx = handler.WithPrincipalCheck(ctx, func(c context.Context) (context.Context, bool) {
		if moved.Load() {
			return tenant.SetTenantID(c, "tenant-b"), true
		}
		return c, true
	})
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(200 * time.Millisecond)
	moved.Store(true)
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("SECURITY: the stream kept tenant-a's feed open for a principal now in tenant-b")
	}
}

// The same close, reached through tenant.ResolveMiddleware rather than a
// hand-built check: the member leaves the tenant, the stream ends at the
// next re-check (2026-10-27 D24).
func TestEventStreamClosesWhenMemberLeaves(t *testing.T) {
	ch, _ := setupTenantInProcHandler(t)
	ch.Events = event.NewEventBus()
	ch.WithEventStreamReauth(time.Second)
	var left atomic.Bool
	h := tenant.ResolveMiddleware(func(context.Context) string {
		if left.Load() {
			return ""
		}
		return "tenant-a"
	})(ch.EventStream())
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1")
	sctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(1500 * time.Millisecond)
	select {
	case <-done:
		t.Fatalf("stream closed while the member still held tenant-a: %s", rec.Body.String())
	default:
	}
	left.Store(true)
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("SECURITY: the stream kept tenant-a's feed open after the member left it")
	}
}
