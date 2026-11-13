package appspec

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// No generated-app builds: each case calls the runtime owner reached by
// generated code, with the same declaration config and mount settings.
func TestBootRefusalParity(t *testing.T) {
	base := func() AppSpec {
		return AppSpec{App: AppSpecApp{Name: "Demo", APIPrefix: "api"}, Entities: []framework.EntityDeclaration{{Name: "tickets", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}}}
	}
	bootEntities := func(t *testing.T, bp AppSpec) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		a := framework.NewApp(framework.WithDB(db), framework.WithAPIPrefix(bp.App.APIPrefix), framework.WithoutDefaultMiddleware())
		for _, d := range bp.Entities {
			cfg, err := d.Config()
			if err != nil {
				t.Fatal(err)
			}
			for _, ep := range bp.Endpoints {
				if ep.Entity == d.Name {
					cfg.Endpoints = append(cfg.Endpoints, framework.Endpoint{Method: ep.Method, Path: ep.Path, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})})
				}
			}
			a.Entity(d.Name, cfg)
		}
		a.EntityUI(entityui.Extensions{})
	}
	newHost := func(a *app.App, baseURL string) *uihost.UIHost {
		return uihost.New(a, uihost.WithStrict(), uihost.WithDescription("Demo app"), uihost.WithFavicon("/favicon.ico"), uihost.WithSitemap(uihost.SitemapConfig{BaseURL: baseURL}), uihost.WithRobots(uihost.RobotsConfig{}))
	}
	cases := []struct {
		name          string
		bad           func(*AppSpec)
		boot          func(*testing.T, AppSpec)
		path, refusal string
	}{
		{"field_default", func(bp *AppSpec) {
			bp.Entities[0].Fields[0] = framework.FieldDeclaration{Name: "quantity", Type: "int", Default: "oops"}
		}, bootEntities, `entity "tickets"`, "invalid Default"},
		{"display", func(bp *AppSpec) { bp.Entities[0].Display = &entity.DisplayConfig{TitleFields: []string{"missing"}} }, bootEntities, `entity "tickets"`, `title_fields[0] names field "missing", which the entity does not declare`},
		{"mcp_without_crud", func(bp *AppSpec) {
			bp.Entities[0].Exposure = &framework.ExposureDeclaration{MCP: true, CRUD: new(false)}
		}, bootEntities, "exposure", "MCP=true with CRUD=false"},
		{"endpoint_events", func(bp *AppSpec) {
			bp.Endpoints = []settings.Endpoint{{Name: "ticket_events", Entity: "tickets", Method: "GET", Path: "_events", Handler: "entities/tickets_endpoints.go#TicketEvents"}}
		}, bootEntities, "endpoints[0]", "generated CRUD route"},
		{"endpoint_siblings", func(bp *AppSpec) {
			bp.Endpoints = []settings.Endpoint{
				{Name: "events", Entity: "tickets", Method: "GET", Path: "events/{id}", Handler: "entities/tickets_endpoints.go#Events"},
				{Name: "history", Entity: "tickets", Method: "GET", Path: "{id}/history", Handler: "entities/tickets_endpoints.go#History"},
			}
		}, bootEntities, "endpoints[1].path", "overlaps"},
		{"base_url", func(bp *AppSpec) { bp.App.BaseURL = "https://example.com?" }, func(t *testing.T, bp AppSpec) { newHost(app.NewApp("demo"), bp.App.BaseURL).Mount(router.New()) }, "app.base_url", "BaseURL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := base()
			tc.bad(&bp)
			if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Errorf("validate accepted boot-refusal case %s or lost spec path %q: %v", tc.name, tc.path, err)
			}
			if got := parityPanic(func() { tc.boot(t, bp) }); !strings.Contains(got, tc.refusal) {
				t.Errorf("runtime refusal = %q; want %q", got, tc.refusal)
			}
		})
	}
}

func TestBaseURLRefusalUsesAppSpecWording(t *testing.T) {
	err := ValidateAppSpec(AppSpec{App: AppSpecApp{Name: "Demo", Module: "example.com/demo", BaseURL: "https://example.com/path"}})
	if err == nil || !strings.Contains(err.Error(), "bare origin, with no path") || strings.Contains(err.Error(), "BasePath") {
		t.Fatalf("app-spec origin remedy: %v", err)
	}
}
