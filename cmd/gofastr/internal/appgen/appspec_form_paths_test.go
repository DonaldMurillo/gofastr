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
	// and an attribute naming __proto__ would read Object.prototype.
	for _, guard := range []string{"if (row[t.key] == null) continue;", "Object.hasOwn(targets, rel)"} {
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
