package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// tenantEchoComp renders the tenant the request ran under, so a test can tell
// "rendered under the granted subject's tenant" from "rendered under whatever
// the ambient middleware left behind" — which is the difference between tenant
// isolation and the absence of it.
type tenantEchoComp struct{}

func (c *tenantEchoComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *tenantEchoComp) RenderCtx(ctx context.Context) render.HTML {
	tid := tenant.GetTenantID(ctx)
	if tid == "" {
		tid = "NONE"
	}
	amb := "none"
	if v, ok := handler.GetTenant(ctx); ok && v != nil {
		amb = "present"
	}
	user := "anonymous"
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		if s, ok := u.(string); ok {
			user = s
		} else {
			user = "non-string-user"
		}
	}
	return render.HTML("<p>tenant:" + tid + "</p><p>ambient:" + amb + "</p><p>user:" + user + "</p>")
}

// tenantFixture builds an embed host whose surface renders the tenant, with an
// optional ambient tenant seeded the way an app's own session middleware would.
func tenantFixture(t *testing.T, mutate func(*fembed.Config)) embedFixture {
	t.Helper()
	application := app.NewApp("Embed Tenant Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	scr := app.NewScreen("/reports", &tenantEchoComp{}).WithTitle("Reports")
	application.RegisterScreen(scr, nil)

	cfg := fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  scr,
			Origins: []string{embedTestOrigin},
			Scopes:  []string{"read"},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve: func(_ context.Context, subject string) (any, error) {
			return subject, nil
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	eh, err := fembed.New(cfg)
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	return embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "reports"}
}

// ResolveTenant must reach the FIRST PAINT, not only the island RPCs.
//
// It was wired into Host.Middleware() alone. The content route builds its own
// context from scratch, so a multi-tenant surface rendered untenanted on first
// paint and tenanted on every swap afterwards — against a fail-closed
// multi-tenant entity, that is exactly the "simply errored" symptom
// Config.ResolveTenant was added to fix, arriving one render later.
func TestEmbedContentInstallsTheResolvedTenant(t *testing.T) {
	f := tenantFixture(t, func(c *fembed.Config) {
		c.ResolveTenant = func(_ context.Context, subject string) (string, error) {
			if subject != "user-7" {
				t.Fatalf("tenant resolver saw subject %q, want the grant's subject", subject)
			}
			return "tenant-of-user-7", nil
		}
	})
	grant := f.grantFor(t, "reports")

	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tenant:tenant-of-user-7") {
		t.Fatalf("first paint did not carry the resolved tenant:\n%s", rec.Body.String())
	}
}

// The ambient tenant must be cleared, exactly as the ambient user is.
//
// Same-site framing (app.acme.com inside www.acme.com) really does send a
// Strict cookie, and an app that derives a tenant from it in its own middleware
// leaves one on the context before this handler runs. Clearing the user and
// keeping the tenant renders the surface as the GRANT's subject scoped to the
// COOKIE visitor's tenant — the pairing Host.Middleware refuses outright.
func TestEmbedContentDoesNotInheritTheAmbientTenant(t *testing.T) {
	f := tenantFixture(t, nil)
	grant := f.grantFor(t, "reports")

	// Seed the ambient tenant the way an app's own session middleware would,
	// by wrapping the host in the middleware that sets it.
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/embed/reports/content", nil)
	req.Header.Set(embedGrantHeader, grant)
	seeded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.SetTenant(r.Context(), "cookie-visitors-tenant")
		ctx = tenant.SetTenantID(ctx, "cookie-visitors-tenant")
		f.host.ServeHTTP(w, r.WithContext(ctx))
	})
	rec := httptest.NewRecorder()
	seeded.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "cookie-visitors-tenant") {
		t.Fatalf("the embed rendered under the ambient tenant:\n%s", body)
	}
	if !strings.Contains(body, "tenant:NONE") {
		t.Fatalf("tenant was not cleared:\n%s", body)
	}
	if !strings.Contains(body, "ambient:none") {
		t.Fatalf("handler.SetTenant was not cleared:\n%s", body)
	}
}

// A tenant resolver that errors fails the render closed. Rendering untenanted
// would either error deep in CRUD or, worse, run unscoped.
func TestEmbedContentFailsClosedWhenTheTenantDoesNotResolve(t *testing.T) {
	f := tenantFixture(t, func(c *fembed.Config) {
		c.ResolveTenant = func(context.Context, string) (string, error) {
			return "", context.DeadlineExceeded
		}
	})
	grant := f.grantFor(t, "reports")

	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d — a failed tenant lookup rendered anyway:\n%s",
			rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// A resolver returning a typed nil must not be installed as a user. The
// middleware has guarded this since v0.49.0 via IsNilValue; the content route
// was still using a bare `user != nil`, so every "is a user present" gate
// downstream reported authenticated for a subject that does not exist.
func TestEmbedContentDoesNotInstallATypedNilUser(t *testing.T) {
	type user struct{ ID string }
	f := tenantFixture(t, func(c *fembed.Config) {
		c.Resolve = func(context.Context, string) (any, error) {
			var u *user // nil, non-nil once boxed in any
			return u, nil
		}
	})
	grant := f.grantFor(t, "reports")

	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if strings.Contains(rec.Body.String(), "non-string-user") {
		t.Fatalf("a typed-nil pointer was installed as the user:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "user:anonymous") {
		t.Fatalf("want the render to see no user:\n%s", rec.Body.String())
	}
}
