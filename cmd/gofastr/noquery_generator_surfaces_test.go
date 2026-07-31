package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func noQueryGeneratorFixture() (framework.EntityDeclaration, cliEntity) {
	decl := framework.EntityDeclaration{
		Name:  "cards",
		Table: "cards",
		Fields: []framework.FieldDeclaration{
			{Name: "number", Type: "string", NoQuery: true},
			{Name: "title", Type: "string"},
		},
	}
	return decl, buildEntityModel(decl, []string{"list"})
}

func TestEntityModelPreservesNoQueryAndSuppressesFilterKinds(t *testing.T) {
	_, model := noQueryGeneratorFixture()
	if len(model.Fields) != 2 {
		t.Fatalf("model fields = %d, want 2", len(model.Fields))
	}
	field := model.Fields[0]
	if !field.NoQuery {
		t.Fatal("buildEntityModel dropped NoQuery")
	}
	if field.Likeable || field.Comparable {
		t.Fatalf("NoQuery field gained filter kinds: %+v", field)
	}
}

func TestCLIListRendererOmitsNoQueryFlagsAndForwarding(t *testing.T) {
	_, model := noQueryGeneratorFixture()
	var source strings.Builder
	renderCLIListVerb(&source, model)
	got := source.String()
	if strings.Contains(got, "fltNumber") || strings.Contains(got, `fs.String("number"`) {
		t.Fatalf("CLI list source exposes NoQuery filter number:\n%s", got)
	}
	if !strings.Contains(got, "fltTitle") {
		t.Fatalf("CLI list source lost queryable title filter:\n%s", got)
	}
}

func TestSDKJSFieldConstantsOmitNoQueryFields(t *testing.T) {
	decl, model := noQueryGeneratorFixture()
	var js, dts strings.Builder
	writeJSEntity(&js, &dts, decl, model)
	got := js.String()
	if strings.Contains(got, `number: "number"`) {
		t.Fatalf("JS query-field constant exposes NoQuery field:\n%s", got)
	}
	if !strings.Contains(got, `title: "title"`) {
		t.Fatalf("JS query-field constant lost queryable field:\n%s", got)
	}
}

func TestSDKJSReadmeExampleSkipsLeadingNoQueryField(t *testing.T) {
	decl, model := noQueryGeneratorFixture()
	got := renderSDKJSReadme(sdkSpec{
		App: "cards", SDKVersion: "test", GofastrVersion: "test",
		BaseURL: "https://example.test", Decls: []framework.EntityDeclaration{decl},
		Entities: []cliEntity{model},
	})
	if strings.Contains(got, "Fields.number") {
		t.Fatalf("SDK README example selects a NoQuery field:\n%s", got)
	}
	if !strings.Contains(got, "Fields.title") {
		t.Fatalf("SDK README example omitted the queryable field:\n%s", got)
	}
}

func TestSDKGoReadmeExampleSkipsLeadingNoQueryField(t *testing.T) {
	decl, model := noQueryGeneratorFixture()
	got := renderSDKGoReadme(sdkSpec{
		App: "cards", SDKVersion: "test", GofastrVersion: "test",
		BaseURL: "https://example.test", Decls: []framework.EntityDeclaration{decl},
		Entities: []cliEntity{model},
	})
	if strings.Contains(got, `params.Set("number"`) {
		t.Fatalf("Go SDK README example selects a NoQuery field:\n%s", got)
	}
	if !strings.Contains(got, `params.Set("title"`) {
		t.Fatalf("Go SDK README example omitted the queryable field:\n%s", got)
	}
}

func TestTypedColumnRendererOmitsNoQueryFields(t *testing.T) {
	decl, _ := noQueryGeneratorFixture()
	got := renderEntityColumns(decl)
	if strings.Contains(got, "CardsNumber") {
		t.Fatalf("typed query column exposes NoQuery field:\n%s", got)
	}
	if !strings.Contains(got, "CardsTitle") {
		t.Fatalf("typed query column lost queryable field:\n%s", got)
	}
}

func TestGeneratedNoQueryResourceBehavior(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module example.com/noquerytest\n\ngo " + goVersion +
		"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}

	bp, err := covT_decode(t, blueprintWithNoQuery+`
screens:
  - name: cards
    route: /cards
    body:
      - kind: entity_list
        entity: cards
        fields: [label, number]
`)
	if err != nil {
		t.Fatal(err)
	}
	bp.App.Module = "example.com/noquerytest"
	bp.App.OutputDir = "gen"
	for _, file := range mustRenderBlueprintFiles(t, bp) {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const generatedTest = `package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui/resource"
)

func TestGeneratedNoQueryResourceBehavior(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE cards (id TEXT PRIMARY KEY, label TEXT, number TEXT); INSERT INTO cards VALUES ('c1','gold','4111')"); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("cards", entity.EntityConfig{Fields: []schema.Field{
		{Name: "label", Type: schema.String},
		{Name: "number", Type: schema.String, NoQuery: true},
	}}.WithTimestamps(false))
	ent.SetDB(db)
	cfg := resource.Config{
		Entity: "cards", Title: "Cards", Singular: "Card", BasePath: "/cards",
		Crud: crud.NewCrudHandler(ent, db),
		Fields: []resource.Field{
			{Key: "label", Label: "Label", Type: "string"},
			{Key: "number", Label: "Number", Type: "string", NoQuery: true},
		},
	}
	html := cfg.List(context.Background()).String()
	if !strings.Contains(html, "4111") {
		t.Fatalf("NoQuery field was not rendered: %s", html)
	}
	if strings.Contains(html, "sort=number") {
		t.Fatalf("NoQuery header rendered a sort link: %s", html)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "gen", "noquery_resource_test.go"), []byte(generatedTest), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-short", "-mod=mod", "-run", "TestGeneratedNoQueryResourceBehavior", "./gen")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated NoQuery resource test failed: %v\n%s", err, output)
	}
}
