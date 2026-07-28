package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// "Install this OUTERMOST" was a contract enforced by nothing. Installed inside
// an authenticator, the header deletions hit an already-spent request while the
// values that authenticator derived sit on the context out of reach — an embed
// running as the grant's subject with the COOKIE user's tenant, which is tenant
// isolation off.
func TestMiddlewareRefusesToRunAfterAnAuthenticator(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	cases := []struct {
		name string
		seed func(context.Context) context.Context
	}{
		{"a user is already resolved", func(ctx context.Context) context.Context {
			return handler.SetUser(ctx, "cookie-user")
		}},
		{"a tenant is already resolved", func(ctx context.Context) context.Context {
			return handler.SetTenant(ctx, "tenant-of-the-cookie")
		}},
		{"a tenant id is already resolved", func(ctx context.Context) context.Context {
			return tenant.SetTenantID(ctx, "tenant-of-the-cookie")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &probe{}
			req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
			req.Header.Set(GrantHeader, grant)
			req = req.WithContext(tc.seed(req.Context()))
			rec := httptest.NewRecorder()
			h.Middleware()(p.handler()).ServeHTTP(rec, req)

			if p.reached {
				t.Fatal("handler ran with a pre-existing identity on the context")
			}
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
			if !strings.Contains(rec.Body.String(), "OUTERMOST") {
				t.Fatalf("body = %q, want it to name the fix", rec.Body.String())
			}
		})
	}
}

// A grant-authenticated response is per-subject. There is no Set-Cookie and no
// Authorization header on these requests, so without an explicit signal a CDN
// sees two different subjects as byte-identical requests.
func TestGrantResponsesAreNotCacheable(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	rec := httptest.NewRecorder()
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if !p.reached {
		t.Fatalf("handler did not run: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want it to forbid storage", got)
	}
	vary := rec.Header().Values("Vary")
	if !containsFold(vary, GrantHeader) {
		t.Fatalf("Vary = %v, want it to include %q", vary, GrantHeader)
	}
}

// Ordinary traffic must not be tarred with the embed response headers — that
// would silently disable caching for the whole app.
func TestOrdinaryRequestsKeepTheirCacheability(t *testing.T) {
	h := middlewareHost(t)
	p := &probe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	rec := httptest.NewRecorder()
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if !p.reached {
		t.Fatal("a request with no grant should pass through untouched")
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("Cache-Control = %q on a non-embed request, want empty", got)
	}
}

// Verification happens once, at entry. A handler that then holds the request
// open — an SSE stream, a long poll — outlives the credential that authorized
// it, and the deadline is the whole answer to "this token lives in a page the
// app does not control".
func TestGrantExpiryBoundsTheRequestContext(t *testing.T) {
	// Long enough that the grant cannot expire between minting and use — a
	// tighter TTL made this test flake by 401ing before the handler ran, which
	// looked exactly like the bug it is meant to catch. Short enough that the
	// assertion below is still specific.
	const ttl = 5 * time.Second
	h := middlewareHost(t, func(c *Config) {
		c.GrantTTL = ttl
		c.GrantMaxAge = time.Minute
	})
	grant := grantFor(t, h)

	var deadline time.Time
	var hadDeadline bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, hadDeadline = r.Context().Deadline()
	})
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(next).ServeHTTP(httptest.NewRecorder(), req)

	if !hadDeadline {
		t.Fatal("request context carried no deadline — a held stream outlives its grant")
	}
	if until := time.Until(deadline); until > ttl {
		t.Fatalf("deadline is %s away, want it bounded by the %s grant TTL", until, ttl)
	}
}

// A resolver written as func(...) (*User, error) that returns a nil *User hands
// back a non-nil interface wrapping nil. Installing it makes every "is a user
// present" gate downstream report authenticated for a subject that does not
// exist.
func TestTypedNilSubjectIsNotInstalledAsAUser(t *testing.T) {
	type user struct{ ID string }
	h := testHost(t, func(c *Config) {
		c.Resolve = func(context.Context, string) (any, error) {
			var u *user // nil, but non-nil once boxed in any
			return u, nil
		}
	})
	grant := grantFor(t, h)

	p := &probe{}
	req := httptest.NewRequest(http.MethodGet, "/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(httptest.NewRecorder(), req)

	if !p.reached {
		t.Fatal("handler did not run")
	}
	if p.user != nil {
		t.Fatalf("context user = %#v, want nil — a typed-nil pointer was installed as a user", p.user)
	}
}

func containsFold(vals []string, want string) bool {
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}
