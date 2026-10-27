package access_test

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// One user, admin in tenant A and viewer in tenant B: a cached answer for A
// must never serve a B request.
func TestResolverKeysByTenant(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(ctx context.Context) []string {
		calls.Add(1)
		if tenant.GetTenantID(ctx) == "A" {
			return []string{"admin"}
		}
		return []string{"viewer"}
	})
	inA := tenant.SetTenantID(resolverCtx("alice"), "A")
	inB := tenant.SetTenantID(resolverCtx("alice"), "B")

	if got := resolver.Resolve(inA); !reflect.DeepEqual(got, []string{"admin"}) {
		t.Fatalf("Resolve(alice@A) = %v", got)
	}
	if got := resolver.Resolve(inB); !reflect.DeepEqual(got, []string{"viewer"}) {
		t.Fatalf("Resolve(alice@B) = %v, want viewer: tenant A's roles served a B request", got)
	}
	_ = resolver.Resolve(inA)
	_ = resolver.Resolve(inB)
	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want one per (tenant, user)", got)
	}

	// Invalidate(user) drops that user's entry in every tenant.
	resolver.Invalidate("alice")
	_ = resolver.Resolve(inA)
	_ = resolver.Resolve(inB)
	if got := calls.Load(); got != 4 {
		t.Fatalf("after Invalidate: calls = %d, want both tenants resolved again", got)
	}
	resolver.InvalidateAll()
	_ = resolver.Resolve(inA)
	if got := calls.Load(); got != 5 {
		t.Fatalf("after InvalidateAll: calls = %d, want 5", got)
	}
}

// The auth-seeded tenant (handler.SetTenant) partitions the cache too, for
// a resolver that reads it rather than the tenant package's slot.
func TestResolverKeysByAuthTenant(t *testing.T) {
	resolver := access.NewCachedResolver(func(ctx context.Context) []string {
		if v, _ := handler.GetTenant(ctx); v == "A" {
			return []string{"admin"}
		}
		return []string{"viewer"}
	})
	_ = resolver.Resolve(handler.SetTenant(resolverCtx("alice"), "A"))
	if got := resolver.Resolve(handler.SetTenant(resolverCtx("alice"), "B")); !reflect.DeepEqual(got, []string{"viewer"}) {
		t.Fatalf("Resolve(alice@B) = %v, want viewer", got)
	}
}
