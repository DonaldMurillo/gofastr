package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// App.Entity mounts an entity's routes at its table, and the generated
// form posted to the entity name: posts on table blog_posts got a form
// whose submit 404'd.
func TestEntityFormPostsToTable(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:   "posts",
		Table:  "blog_posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}
	expr := appSpecEntityFormExpr(appspec.AppSpec{},
		appspec.AppSpecScreen{Name: "home"},
		appspec.AppSpecBlock{Kind: "entity_form", Entity: "posts"},
		nil, map[string]framework.EntityDeclaration{"posts": decl}, "/api",
	)
	if !strings.Contains(expr, `interactive.Post("/api/blog_posts")`) {
		t.Errorf("the form does not post to the table's route:\n%s", expr)
	}
}

func TestEntityFormInvalidatesDependentScreens(t *testing.T) {
	files := mustRenderAppSpecFiles(t, freshnessAppSpec(t))
	src := fileContent(files, "screen_notes_crud.go")
	for _, selector := range []string{"/", "/mirror"} {
		if !strings.Contains(src, `invalidateScreensOnWrite(fwApp, "/api/memos", "`+selector+`")`) {
			t.Errorf("generated screens lack %s invalidation", selector)
		}
	}
	var helpers int
	for _, file := range files {
		helpers += strings.Count(file.Content, "func invalidateScreensOnWrite(")
		if file.Name != "screens_register.go" && strings.Contains(file.Content, "fwApp.Use(func(next http.Handler)") {
			t.Errorf("%s duplicates the shared invalidation middleware", file.Name)
		}
	}
	if helpers != 1 {
		t.Errorf("generated app has %d invalidation helpers, want one", helpers)
	}
	shared := fileContent(files, "screens_register.go")
	if !strings.Contains(shared, `r.URL.Path == api || strings.HasPrefix(r.URL.Path, api+"/")`) || !strings.Contains(shared, `ui.InvalidateScreens(w, paths...)`) {
		t.Error("shared invalidation must match the table's API route and record mutations")
	}
	form := fileContent(files, "screen_note_new.go")
	if !strings.Contains(form, `OnSuccess(interactive.ResetForm())`) || strings.Contains(form, `interactive.Navigate(`) {
		t.Error("authored create must reset and stay")
	}
	if strings.Contains(form, "invalidateScreensOnWrite(") {
		t.Error("a create-only screen does not render entity data")
	}
}

func TestStandaloneScreenInvalidatesEntityViews(t *testing.T) {
	bp := freshnessAppSpec(t)
	screen := appspec.AppSpecScreen{Name: "edit", Route: "/notes/{id}/edit", Body: []appspec.AppSpecBlock{{Kind: "entity_form", Entity: "notes", Mode: "edit"}}}
	entities := map[string]framework.EntityDeclaration{"notes": bp.Entities[0]}
	src := renderAppSpecStandaloneScreenFile(screen, bp, entities, "/api", 0).Content
	if !strings.Contains(src, `invalidateScreensOnWrite(fwApp, "/api/memos", "*")`) {
		t.Error("authored edit view must wire dependent-screen invalidation on the table's mutation route")
	}
}

func TestScreenInvalidationDependencies(t *testing.T) {
	bp := appspec.AppSpec{
		Entities: []framework.EntityDeclaration{{Name: "posts", Table: "blog_posts"}},
	}
	bp.App.APIPrefix = "/v2"
	for _, kind := range []string{"entity_list", "entity_detail", "entity_form"} {
		screen := appspec.AppSpecScreen{Route: "/posts/{id}", Body: []appspec.AppSpecBlock{{Kind: "section", Children: []appspec.AppSpecBlock{{Kind: kind, Entity: "posts", Mode: "edit"}}}}}
		src := appSpecScreenInvalidation(screen, bp)
		if !strings.Contains(src, `invalidateScreensOnWrite(fwApp, "/v2/blog_posts", "*")`) {
			t.Errorf("%s record routes must invalidate their cached instances using the table: %s", kind, src)
		}
		if !strings.Contains(appSpecScreensRegisterGo, `case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:`) {
			t.Errorf("%s must cover writes, not reads: %s", kind, src)
		}
	}
	for _, blocks := range [][]appspec.AppSpecBlock{nil, {{Kind: "entity_form", Entity: "posts"}}, {{Kind: "entity_list", Entity: "missing"}}} {
		if src := appSpecScreenInvalidation(appspec.AppSpecScreen{Route: "/", Body: blocks}, bp); src != "" {
			t.Errorf("a screen with no readable entity dependency emitted invalidation: %s", src)
		}
	}
}

// A relation select fetched its target by entity name and set each
// option's value to row.id. A target keyed by currency_code carries
// currencyCode and no id, so every option posted undefined; a target on
// another table answered 404.
func TestRelationSelectUsesTargetKey(t *testing.T) {
	entities := map[string]framework.EntityDeclaration{
		"currencies": {
			Name:       "currencies",
			PrimaryKey: []string{"currency_code"},
			Fields:     []framework.FieldDeclaration{{Name: "currency_code", Type: "string", Required: true}},
		},
		"vendors": {
			Name:   "vendors",
			Table:  "shop_vendors",
			Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		},
		"notes": {Name: "notes", Fields: []framework.FieldDeclaration{{Name: "body", Type: "text"}}},
		// Two key columns: no one value a relation column can hold.
		"pairs": {
			Name:       "pairs",
			PrimaryKey: []string{"a", "b"},
			Fields:     []framework.FieldDeclaration{{Name: "a", Type: "string"}, {Name: "b", Type: "string"}},
		},
		"products": {
			Name: "products",
			Fields: []framework.FieldDeclaration{
				{Name: "currency", Type: "relation", To: "currencies"},
				{Name: "vendor", Type: "relation", To: "vendors"},
				{Name: "pair", Type: "relation", To: "pairs"},
				{Name: "ghost", Type: "relation", To: "ghosts"},
				{Name: "note", Type: "string", To: "notes"},
			},
		},
	}
	js := appSpecEntityFormClientJS(appspec.AppSpecBlock{Kind: "entity_form", Entity: "products"}, entities, "/api")
	want := `{"currencies":{"key":"currencyCode","path":"/api/currencies"},"vendors":{"key":"id","path":"/api/shop_vendors"}}`
	if !strings.Contains(js, "const targets = "+want+";") {
		t.Errorf("relation targets are not the tables and keys %s:\n%s", want, js)
	}
	if strings.Contains(js, "row.id;") || strings.Contains(js, "apiBase + '/' + target") {
		t.Errorf("the select still reads row.id or fetches by entity name:\n%s", js)
	}
	// A row without the key would become an option posting "undefined",
	// and an attribute naming __proto__ would read Object.prototype. The
	// fetch goes through a native host's transport when one is installed.
	for _, guard := range []string{"if (row[t.key] == null) continue;", "Object.hasOwn(targets, rel)",
		"((window.__gofastr && window.__gofastr.fetch) || fetch)(t.path"} {
		if !strings.Contains(js, guard) {
			t.Errorf("the select lost its guard %q:\n%s", guard, js)
		}
	}
}

// The e2e test's CRUD target calls the same routes, so it must use the
// table too.
func TestE2ETargetUsesTable(t *testing.T) {
	bp := appspec.AppSpec{
		Entities: []framework.EntityDeclaration{{
			Name: "posts", Table: "blog_posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []appspec.AppSpecScreen{{
			Name: "posts", Route: "/posts",
			Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "posts", Create: true}},
		}},
	}
	got, ok := appSpecE2EWritableTarget(bp)
	if !ok {
		t.Fatal("no e2e target")
	}
	if got.apiPath != "/blog_posts" {
		t.Errorf("e2e target path = %q, want /blog_posts", got.apiPath)
	}
}
