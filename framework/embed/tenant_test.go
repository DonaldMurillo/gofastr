package embed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// tenantProbe records the tenant the handler actually ran under.
type tenantProbe struct {
	reached  bool
	tenantID string
	ctxValue any
}

func (p *tenantProbe) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.reached = true
		p.tenantID = tenant.GetTenantID(r.Context())
		p.ctxValue, _ = handler.GetTenant(r.Context())
	})
}

// A multi-tenant entity behind an embed used to error, because the middleware
// cleared the tenant and offered no way to install the right one.
func TestResolveTenantInstallsTheSubjectsTenant(t *testing.T) {
	h := testHost(t, func(c *Config) {
		c.Resolve = func(_ context.Context, subject string) (any, error) { return subject, nil }
		c.ResolveTenant = func(_ context.Context, subject string) (string, error) {
			if subject != "u-7" {
				t.Fatalf("resolver saw subject %q, want the grant's subject", subject)
			}
			return "tenant-of-u-7", nil
		}
	})
	grant := grantFor(t, h)

	p := &tenantProbe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(httptest.NewRecorder(), req)

	if !p.reached {
		t.Fatal("handler did not run")
	}
	if p.tenantID != "tenant-of-u-7" {
		t.Fatalf("tenant.GetTenantID = %q, want the resolved tenant", p.tenantID)
	}
	if p.ctxValue != "tenant-of-u-7" {
		t.Fatalf("handler.GetTenant = %v, want the resolved tenant", p.ctxValue)
	}
}

// The tenant comes from a server-side lookup on the SUBJECT. Anything the
// request carried must be unable to influence it — that is the property that
// makes this safe to add at all.
func TestRequestCannotInfluenceTheResolvedTenant(t *testing.T) {
	h := testHost(t, func(c *Config) {
		c.ResolveTenant = func(context.Context, string) (string, error) { return "correct-tenant", nil }
	})
	grant := grantFor(t, h)

	p := &tenantProbe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	// Everything an attacker controls, all claiming a different tenant.
	req.Header.Set("X-Tenant-ID", "attacker-tenant")
	req.Header.Set("X-Tenant", "attacker-tenant")
	req.Header.Set("Cookie", "tenant=attacker-tenant")
	h.Middleware()(p.handler()).ServeHTTP(httptest.NewRecorder(), req)

	if p.tenantID != "correct-tenant" {
		t.Fatalf("tenant = %q — the request influenced it", p.tenantID)
	}
}

// A resolver that errors fails the request closed. Running untenanted would
// either error deep in CRUD or, worse, run across tenants.
func TestResolveTenantErrorFailsClosed(t *testing.T) {
	h := testHost(t, func(c *Config) {
		c.ResolveTenant = func(context.Context, string) (string, error) {
			return "", errors.New("tenant lookup down")
		}
	})
	grant := grantFor(t, h)

	p := &tenantProbe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	rec := httptest.NewRecorder()
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if p.reached {
		t.Fatal("handler ran after the tenant lookup failed")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// Nil ResolveTenant behaves exactly as before: no tenant, and the cookie's
// tenant still does not leak in.
func TestWithoutResolveTenantNothingIsInstalled(t *testing.T) {
	h := testHost(t)
	grant := grantFor(t, h)

	p := &tenantProbe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	req.Header.Set("Cookie", "tenant=attacker-tenant")
	h.Middleware()(p.handler()).ServeHTTP(httptest.NewRecorder(), req)

	if !p.reached {
		t.Fatal("handler did not run")
	}
	if p.tenantID != "" {
		t.Fatalf("tenant = %q, want empty when no resolver is configured", p.tenantID)
	}
}

// An empty tenant id is "no tenant", not an error — a resolver may legitimately
// say a subject is untenanted.
func TestEmptyTenantIsNotAnError(t *testing.T) {
	h := testHost(t, func(c *Config) {
		c.ResolveTenant = func(context.Context, string) (string, error) { return "", nil }
	})
	grant := grantFor(t, h)

	p := &tenantProbe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(httptest.NewRecorder(), req)

	if !p.reached {
		t.Fatal("an untenanted subject should still be served")
	}
	if p.tenantID != "" {
		t.Fatalf("tenant = %q, want empty", p.tenantID)
	}
}
