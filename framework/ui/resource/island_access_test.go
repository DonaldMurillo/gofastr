package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// gatedSource is a DataSource that also answers the entity read gate, the
// way *crud.CrudHandler does.
type gatedSource struct {
	stubSource
	canRead bool
}

func (g *gatedSource) CanRead(context.Context) bool { return g.canRead }

// The production type (*crud.CrudHandler) implements BOTH predicates, and
// canRead/canReadCrud prefer CanReadScoped. A stub offering only the narrow one
// would exercise the fallback branch instead of the path real apps take.
func (g *gatedSource) CanReadScoped(context.Context) bool { return g.canRead }

func islandConfig(src DataSource) Config {
	return Config{
		Entity:   "secrets",
		Title:    "Secrets",
		Singular: "Secret",
		BasePath: "/secrets",
		APIPath:  "/api/secrets",
		Crud:     src,
		Fields:   []Field{{Key: "name", Label: "Name", Type: "string"}},
	}.WithIsland("/api/tables/secrets")
}

func islandRequest(t *testing.T, cfg Config, user any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tables/secrets", nil)
	if user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), user))
	}
	rr := httptest.NewRecorder()
	cfg.TableHandler().ServeHTTP(rr, req)
	return rr
}

// The island endpoint is a second route onto the rows the screen shows. A
// role gate that lives only on the screen route is not a gate: without this,
// any signed-in user could read an admin-only list by calling the island
// path the page's own sort header points at.
func TestIslandEnforcesScreenPolicy(t *testing.T) {
	rows := []map[string]any{{"id": "s-1", "name": "LAUNCH CODES"}}

	adminOnly := appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		u, _ := handler.GetUser(ctx)
		if s, ok := u.(string); ok && s == "admin" {
			return appui.Decision{Kind: appui.DecisionAllow}
		}
		return appui.Decision{Kind: appui.DecisionBlock, Status: http.StatusForbidden, Message: "Forbidden"}
	})
	cfg := islandConfig(&stubSource{rows: rows}).WithIslandPolicy(adminOnly)

	if rr := islandRequest(t, cfg, "member"); rr.Code != http.StatusForbidden {
		t.Errorf("non-admin got %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	} else if strings.Contains(rr.Body.String(), "LAUNCH CODES") {
		t.Error("blocked response leaked row data")
	}

	rr := islandRequest(t, cfg, "admin")
	if rr.Code != http.StatusOK {
		t.Fatalf("admin got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "LAUNCH CODES") {
		t.Error("admin response is missing the rows")
	}
}

// A policy outcome that only makes sense for a document navigation cannot be
// honoured by a fragment RPC. Refuse rather than serve rows the policy
// declined to show.
func TestIslandRefusesRedirectPolicy(t *testing.T) {
	redirecting := appui.PolicyFunc(func(context.Context) appui.Decision {
		return appui.Decision{Kind: appui.DecisionRedirect, URL: "/login"}
	})
	cfg := islandConfig(&stubSource{rows: []map[string]any{{"id": "s-1", "name": "LAUNCH CODES"}}}).
		WithIslandPolicy(redirecting)

	rr := islandRequest(t, cfg, "member")
	if rr.Code != http.StatusForbidden {
		t.Errorf("redirect decision got %d, want 403", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "LAUNCH CODES") {
		t.Error("redirected response leaked row data")
	}
}

// The island must enforce the entity's declared read permission — the same
// one GET /api/secrets enforces. Otherwise it answers 200 for rows the JSON
// API answers 403 for.
func TestIslandEnforcesEntityReadPermission(t *testing.T) {
	rows := []map[string]any{{"id": "s-1", "name": "LAUNCH CODES"}}

	denied := islandConfig(&gatedSource{stubSource: stubSource{rows: rows}, canRead: false})
	if rr := islandRequest(t, denied, "member"); rr.Code != http.StatusForbidden {
		t.Errorf("without the read permission got %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	} else if strings.Contains(rr.Body.String(), "LAUNCH CODES") {
		t.Error("denied response leaked row data")
	}

	allowed := islandConfig(&gatedSource{stubSource: stubSource{rows: rows}, canRead: true})
	if rr := islandRequest(t, allowed, "member"); rr.Code != http.StatusOK {
		t.Errorf("with the read permission got %d, want 200", rr.Code)
	}
}

// A DataSource that does not answer the read gate keeps working — the
// permission check is an optional interface, not a new requirement.
func TestIslandAllowsUngatedDataSource(t *testing.T) {
	cfg := islandConfig(&stubSource{rows: []map[string]any{{"id": "s-1", "name": "Ada"}}})
	if rr := islandRequest(t, cfg, "member"); rr.Code != http.StatusOK {
		t.Errorf("ungated source got %d, want 200", rr.Code)
	}
}

// The island fragment must consult the FULL read posture, not RBAC alone.
// A default-posture entity (no OwnerField, no Access, no Public) passes the
// RBAC-only check while its JSON route answers 401, so gating on CanRead let
// the fragment serve rows the API refuses.
//
// This test exists partly because the fix was silently reverted once: a
// mutation-testing restore of an older file copy took it out, and nothing
// failed.
func TestIslandHandlerUsesFullReadPosture(t *testing.T) {
	src := &defaultPostureSource{stubSource: stubSource{rows: []map[string]any{
		{"id": "s1", "name": "session-required"},
	}}}
	cfg := islandConfig(src)

	// A SIGNED-IN caller: the handler's own session check passes, so the only
	// thing that can refuse is the posture check. With an anonymous caller the
	// session check answers 401 first and the two predicates are
	// indistinguishable — which is why this case, not that one, is the test.
	rr := islandRequest(t, cfg, struct{ ID string }{ID: "u1"})
	if rr.Code != http.StatusForbidden {
		t.Errorf("island fragment = %d for a signed-in caller, want 403 — the scoped posture refuses even though the RBAC-only check passes", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "session-required") {
		t.Errorf("island fragment served a row the JSON route refuses:\n%s", rr.Body.String())
	}
}
