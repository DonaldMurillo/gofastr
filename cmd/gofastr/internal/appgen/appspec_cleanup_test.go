package appgen

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestGeneratedCatchAllMountMatchesRouter(t *testing.T) {
	screen := appspec.AppSpecScreen{Name: "docs", Route: "/docs/{path...}"}
	router := app.NewRouter()
	mounted := &app.Screen{Path: screen.Route, Component: screenRouteProbe{}}
	router.Screen(mounted, nil)
	mount := AppSpecScreenMountStmt(screen, appspec.AppSpec{})
	if !strings.Contains(mount, strconv.Quote(mounted.Path)) {
		t.Errorf("generated mount %q does not register the router pattern %q", mount, mounted.Path)
	}
}

func TestAddEquivalentScreenRoutesRefused(t *testing.T) {
	for _, tc := range []struct {
		name, existing, fragment, collision string
	}{
		{
			name: "authored", existing: "/docs/:old",
			fragment:  "screens:\n  - name: newdocs\n    route: /docs/{slug}\n    title: Docs\n",
			collision: "newdocs",
		},
		{
			name: "incoming synthesized", existing: "/docs/:old/create",
			fragment:  "screens:\n  - name: posts\n    route: /docs/{slug}\n    title: Posts\n    body:\n      - kind: entity_list\n        entity: posts\n        fields: [title]\n        create: true\n",
			collision: "posts_create",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := addTestAppSpec("example.com/addtest") + "screens:\n  - name: existing\n    route: " + tc.existing + "\n    title: Existing\n"
			dir, spec := clitest.AddSetup(t, base)
			clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec}) })
			fragment := filepath.Join(dir, "fragment.yml")
			clitest.WriteTestFile(t, fragment, tc.fragment)
			code := 0
			out := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() {
					GenerateFromAppSpec(GenerateOptions{Spec: fragment, add: true, json: true})
				})
			})
			if code != 1 || !strings.Contains(out, tc.collision) || !strings.Contains(out, "collides with a route already mounted") {
				t.Errorf("add exit %d, want collision for %s: %s", code, tc.collision, out)
			}
		})
	}
}

func TestNavHrefRequiresGET(t *testing.T) {
	bp := appspec.AppSpec{
		App:       appspec.AppSpecApp{Auth: appspec.AppSpecAuth{Enabled: true, BasePath: "/session"}},
		Entities:  []framework.EntityDeclaration{{Name: "posts"}},
		Endpoints: []settings.Endpoint{{Entity: "posts", Path: "custom/submit", Method: "POST"}, {Entity: "posts", Path: "custom/read", Method: "GET"}},
		Nav:       []appspec.AppSpecNavItem{{Href: "/session/logout"}, {Href: "/session/me"}, {Href: "/posts/custom/submit"}, {Href: "/posts/custom/read"}, {Href: "/login"}},
	}
	got := LintNavHrefs(bp, "")
	if len(got) != 2 || got[0].Href != "/session/logout" || got[1].Href != "/posts/custom/submit" {
		t.Errorf("findings = %+v, want only POST-only auth and custom endpoints", got)
	}
}

func TestPostFormFollowsMountedScreenPatterns(t *testing.T) {
	for _, tc := range []struct {
		name, route, action string
		want                bool
	}{
		{"catch-all", "/docs/{rest...}", "/docs/install/linux", true},
		{"typed match", "/orders/{id:int}", "/orders/42?x=1", true},
		{"typed miss", "/orders/{id:int}", "/orders/abc", false},
		{"escaped segment", "/orders/{id}", "/orders/a%2Fb", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bp := appspec.AppSpec{Screens: []appspec.AppSpecScreen{
				{Name: "sender", Route: "/send", Body: []appspec.AppSpecBlock{{Kind: "custom_form", Props: map[string]any{"action": tc.action}}}},
				{Name: "target", Route: tc.route},
			}}
			got := LintPostFormsOnScreens(bp)
			if (len(got) != 0) != tc.want {
				t.Errorf("findings = %+v, want warning %t", got, tc.want)
			}
		})
	}
	t.Run("composite key mount", func(t *testing.T) {
		bp := appspec.AppSpec{
			Entities: []framework.EntityDeclaration{{
				Name: "orders", PrimaryKey: []string{"store", "number"},
			}},
			Screens: []appspec.AppSpecScreen{
				{Name: "sender", Route: "/send", Body: []appspec.AppSpecBlock{{Kind: "custom_form", Props: map[string]any{"action": "/orders/shop/42"}}}},
				{Name: "target", Route: "/orders/{id}", Body: []appspec.AppSpecBlock{{Kind: "entity_detail", Entity: "orders"}}},
			},
		}
		got := LintPostFormsOnScreens(bp)
		if len(got) != 1 || got[0].Route != "/orders/{id}" {
			t.Errorf("composite key mount findings = %+v, want warning on /orders/{id}", got)
		}
	})

}

func TestCreateFormsOwnMountActionsAndControlIDs(t *testing.T) {
	form := appspec.AppSpecBlock{Kind: "entity_form", Entity: "posts"}
	screen := appspec.AppSpecScreen{Name: "forms", Body: []appspec.AppSpecBlock{
		form, {Kind: "stack", Children: []appspec.AppSpecBlock{form}},
	}}
	entities := map[string]framework.EntityDeclaration{
		"posts":      {Name: "posts", Fields: []framework.FieldDeclaration{{Name: "category_id", Type: "relation", To: "categories"}}},
		"categories": {Name: "categories"},
	}
	actions := screenActions(screen, entities, "/api")
	if len(actions) != 2 {
		t.Fatalf("got %d mount actions, want 2", len(actions))
	}
	if actions[0].Name == actions[1].Name {
		t.Errorf("both forms use mount action %q", actions[0].Name)
	}
	idPattern := regexp.MustCompile(`ID: "([^"]+)"`)
	ids := map[string]bool{}
	for i, path := range [][]int{{0}, {1, 0}} {
		src := appSpecEntityFormExpr(appspec.AppSpec{}, screen, form, path, entities, "/api")
		match := idPattern.FindStringSubmatch(src)
		if len(match) != 2 || ids[match[1]] {
			t.Errorf("form %v emitted a missing or duplicate control ID: %v", path, match)
		} else {
			ids[match[1]] = true
		}
		name := actions[i].Name
		if !strings.Contains(src, `"data-action-mount": "`+name+`"`) || !strings.Contains(actions[i].ClientJS, `[data-action-mount="`+name+`"]`) {
			t.Errorf("form %v mount action %q is not bound to its own marker", path, name)
		}
	}
}

func TestMarkdownH1LowersFollowingListHeading(t *testing.T) {
	list := appspec.AppSpecBlock{Kind: "entity_list", Entity: "posts", Text: "Posts"}
	screen := appspec.AppSpecScreen{Name: "home", Body: []appspec.AppSpecBlock{
		{Kind: "stack", Children: []appspec.AppSpecBlock{{Type: "markdown", Text: "# Overview"}}}, list,
	}}
	if !AppSpecScreenDrawsH1(screen.Body[:1]) {
		t.Fatal("Markdown must suppress the fallback H1")
	}
	if h1, done := appSpecH1Before(screen.Body, list.Entity); !h1 || !done {
		t.Errorf("Markdown H1 before list = %t, reached a heading or list = %t", h1, done)
	}
}

func TestScaffoldCSSUsesOwnedSheets(t *testing.T) {
	bp := appspec.AppSpec{
		App:     appspec.AppSpecApp{Name: "Styles", Module: "example.com/styles", StaticDir: "static"},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Title: "Home"}},
	}
	files := filesByName(mustRenderAppSpecFiles(t, bp))
	clitest.AssertContains(t, files["main.go"], "WithCustomCSS(fontFaceCSS)")
	clitest.AssertContains(t, files["main.go"], ".style.css")
	for _, name := range []string{"main.go", "app.go"} {
		for _, stale := range []string{"appBaseCSS", "ReadCustomCSSFile"} {
			if strings.Contains(files[name], stale) {
				t.Errorf("%s still emits %s", name, stale)
			}
		}
	}
}
