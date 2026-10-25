package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

// covT_richAppSpec builds an app spec exercising entity_list blocks,
// islands, widgets, node-renderer blocks with props/children/actions,
// postgres driver, screens with actions, and an empty-body screen.
func covT_richAppSpec() appspec.AppSpec {
	return appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:      "Rich",
			Module:    "example.com/rich",
			StaticDir: "public",
			DBDriver:  "postgres",
			DBURL:     "postgres://localhost/rich",
			OutputDir: "./gen",
			Theme:     map[string]string{"primary": "#fff"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []appspec.AppSpecScreen{
			{
				Name:  "home",
				Title: "Home",
				Type:  "page",
				Body: []appspec.AppSpecBlock{
					{Type: "heading", Level: 2, Text: "Posts", Props: map[string]any{"x": "y"},
						Actions:  []appspec.AppSpecAction{{Name: "save", Event: "input", ClientJS: "x()"}, {Name: "alt", Event: "click"}},
						Children: []appspec.AppSpecBlock{{Type: "p", Text: "child"}}},
					{Kind: "entity_list", Entity: "posts", Limit: 5, EmptyText: "Nothing", Text: "All Posts"},
					{Type: "p", Text: "island", Island: "live"},
					{Type: "p", Text: "widget", Widget: "card"},
					{Type: "link", Href: "/x", Text: "Go"},
				},
			},
			{Name: "empty", Title: "Empty", Type: "drawer"}, // no body → html.Heading path
		},
		Endpoints:  []settings.Endpoint{{Name: "archive", Entity: "posts", Method: "POST", Path: "{id}/archive", Handler: "entities/posts_endpoints.go#ArchivePost"}},
		Middleware: []appspec.AppSpecNamedStub{{Name: "logging"}},
		Plugins:    []appspec.AppSpecNamedStub{{Name: "metrics"}},
		Helpers:    []appspec.AppSpecNamedStub{{Name: "fmtx"}},
	}
}

func TestRenderAppSpecFilesRichShape(t *testing.T) {
	files, err := appgen.RenderAppSpecFiles(covT_richAppSpec())
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Name] = f.Content
	}
	if _, ok := got["main.go"]; !ok {
		t.Fatal("expected main.go")
	}
	screens := allScreenContent(files)
	for _, want := range []string{"HomeScreen", "EmptyScreen", `appUI.List("posts")`, "island.NewIsland", "component.NewWidget", "ui.Link", "uinode.Node"} {
		if !strings.Contains(screens, want) {
			t.Errorf("screens.go missing %q", want)
		}
	}
	// Generated apps must render node trees via the leaf core-ui/noderender
	// package (core-ui/html + core/render + core-ui/node only), NOT kiln/render,
	// which drags Kiln's authoring engine (kiln/expr, kiln/effect, framework)
	// into a shipped app.
	if !strings.Contains(screens, "core-ui/noderender") {
		t.Errorf("screens.go should import core-ui/noderender:\n%s", screens)
	}
	if strings.Contains(screens, `"github.com/DonaldMurillo/gofastr/kiln/render"`) {
		t.Errorf("screens.go must NOT import the heavy kiln/render package")
	}
	// The endpoint lands in the settings file with its stub and registration.
	for name, want := range map[string]string{
		"gofastr.settings.yml":        "handler: \"entities/posts_endpoints.go#ArchivePost\"",
		"entities/posts_endpoints.go": "func ArchivePost(ctx context.Context, in ArchivePostInput) (*Posts, error)",
		"entities/endpoints.gen.go":   `app.EntityEndpoints("posts"`,
		"entities/client/client.go":   ") Archive(ctx context.Context, id string)",
	} {
		if !strings.Contains(got[name], want) {
			t.Errorf("%s missing %q:\n%s", name, want, got[name])
		}
	}
	main := got["main.go"]
	if !strings.Contains(main, "lib/pq") {
		t.Fatalf("postgres main should import lib/pq:\n%s", main)
	}
}
