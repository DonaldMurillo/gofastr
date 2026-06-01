package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

func covT_f64(v float64) *float64 { return &v }
func covT_bool(v bool) *bool      { return &v }

func TestRelationTypeConstAll(t *testing.T) {
	cases := map[framework.RelationType]string{
		framework.RelHasOne:     "RelHasOne",
		framework.RelHasMany:    "RelHasMany",
		framework.RelManyToOne:  "RelManyToOne",
		framework.RelManyToMany: "RelManyToMany",
		framework.RelationType(99): "RelManyToOne",
	}
	for in, want := range cases {
		if got := relationTypeConst(in); got != want {
			t.Errorf("relationTypeConst(%d)=%q want %q", in, got, want)
		}
	}
}

func TestSchemaConstNameAll(t *testing.T) {
	good := map[string]string{
		"": "String", "string": "String", "text": "Text", "int": "Int", "integer": "Int",
		"float": "Float", "number": "Float", "decimal": "Decimal", "bool": "Bool",
		"boolean": "Bool", "enum": "Enum", "uuid": "UUID", "timestamp": "Timestamp",
		"datetime": "Timestamp", "date": "Date", "json": "JSON", "relation": "Relation",
		"image": "Image", "file": "File",
	}
	for in, want := range good {
		got, err := schemaConstName(in)
		if err != nil || got != want {
			t.Errorf("schemaConstName(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := schemaConstName("bogus"); err == nil {
		t.Fatal("expected error for bogus type")
	}
}

func TestAutoGenerateConstNameAll(t *testing.T) {
	good := map[string]string{
		"": "AutoNone", "none": "AutoNone", "uuid": "AutoUUID", "timestamp": "AutoTimestamp",
		"increment": "AutoIncrement", "auto_increment": "AutoIncrement",
	}
	for in, want := range good {
		got, err := autoGenerateConstName(in)
		if err != nil || got != want {
			t.Errorf("autoGenerateConstName(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := autoGenerateConstName("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGoTypeForFieldAll(t *testing.T) {
	cases := map[string]string{
		"int": "int", "integer": "int", "float": "float64", "number": "float64",
		"bool": "bool", "boolean": "bool", "json": "map[string]any", "string": "string", "x": "string",
	}
	for in, want := range cases {
		if got := goTypeForField(in); got != want {
			t.Errorf("goTypeForField(%q)=%q want %q", in, got, want)
		}
	}
}

func TestToCamelJSONAndSnake(t *testing.T) {
	if toCamelJSON("") != "" {
		t.Fatal("empty")
	}
	if toCamelJSON("user_name") != "userName" {
		t.Fatalf("got %q", toCamelJSON("user_name"))
	}
	if toSnakeCase("UserName") != "user_name" {
		t.Fatalf("got %q", toSnakeCase("UserName"))
	}
}

func TestRenderGoLiteralAllTypes(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{true, "true"},
		{"hi", `"hi"`},
		{float64(3), "3"},
		{float64(3.5), "3.5"},
		{42, "42"},
		{int64(7), "7"},
		{[]string{"a", "b"}, `[]string{"a", "b"}`},
		{[]any{float64(1), "x"}, `[]any{1, "x"}`},
		{map[string]any{"k": "v"}, `map[string]any{"k": "v"}`},
	}
	for _, c := range cases {
		got, err := renderGoLiteral(c.in)
		if err != nil || got != c.want {
			t.Errorf("renderGoLiteral(%v)=%q,%v want %q", c.in, got, err, c.want)
		}
	}
	if _, err := renderGoLiteral(struct{}{}); err == nil {
		t.Fatal("expected error for unsupported type")
	}
	// nested error propagation
	if _, err := renderGoLiteral([]any{struct{}{}}); err == nil {
		t.Fatal("expected nested slice error")
	}
	if _, err := renderGoLiteral(map[string]any{"k": struct{}{}}); err == nil {
		t.Fatal("expected nested map error")
	}
}

func TestRenderIndexLiteral(t *testing.T) {
	got := renderIndexLiteral(framework.Index{Name: "ix", Columns: []string{"a", "b"}, Unique: true})
	for _, want := range []string{`Name: "ix"`, `Columns: []string{"a", "b"}`, "Unique: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("index literal missing %q: %s", want, got)
		}
	}
	if got := renderIndexLiteral(framework.Index{}); got != "{}" {
		t.Fatalf("empty index: %s", got)
	}
}

func TestRenderRelationLiteralFull(t *testing.T) {
	got := renderRelationLiteral(framework.Relation{
		Type: framework.RelManyToMany, Name: "tags", Entity: "tag",
		ForeignKey: "fk", Through: "jt", LocalKey: "lk", ForeignKeyTarget: "fkt",
	})
	for _, want := range []string{"RelManyToMany", `Name: "tags"`, `Entity: "tag"`, `ForeignKey: "fk"`, `Through: "jt"`, `LocalKey: "lk"`, `ForeignKeyTarget: "fkt"`} {
		if !strings.Contains(got, want) {
			t.Errorf("relation missing %q: %s", want, got)
		}
	}
}

func TestRenderFieldLiteralFull(t *testing.T) {
	got, err := renderFieldLiteral(framework.FieldDeclaration{
		Name: "score", Type: "float", Required: true, Unique: true, ReadOnly: true,
		Hidden: true, Default: float64(1), AutoGenerate: "uuid",
		Max: covT_f64(10), Min: covT_f64(0), Pattern: "^x$",
		Values: []string{"a", "b"}, To: "user", Many: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Required: true", "Unique: true", "ReadOnly: true", "Hidden: true",
		"Default: 1", "AutoGenerate: schema.AutoUUID", "Max: floatPtr(10)", "Min: floatPtr(0)",
		`Pattern: "^x$"`, `Values: []string{"a", "b"}`, `To: "user"`, "Many: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("field literal missing %q: %s", want, got)
		}
	}
}

func TestRenderFieldLiteralErrors(t *testing.T) {
	if _, err := renderFieldLiteral(framework.FieldDeclaration{Name: "x", Type: "bogus"}); err == nil {
		t.Fatal("bad type")
	}
	if _, err := renderFieldLiteral(framework.FieldDeclaration{Name: "x", Type: "string", AutoGenerate: "bogus"}); err == nil {
		t.Fatal("bad autogen")
	}
	if _, err := renderFieldLiteral(framework.FieldDeclaration{Name: "x", Type: "string", Default: struct{}{}}); err == nil {
		t.Fatal("bad default")
	}
}

func TestRenderEntityRegistrationFull(t *testing.T) {
	got, err := renderEntityRegistration(framework.EntityDeclaration{
		Name: "user", Table: "users",
		Fields:       []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		Relations:    []framework.Relation{{Type: framework.RelHasMany, Name: "posts", Entity: "post"}},
		SoftDelete:   true, MultiTenant: true, CRUD: covT_bool(true), MCP: true,
		CursorField:  "id", CursorFields: []string{"a", "b"},
		Indices:      []framework.Index{{Name: "ix", Columns: []string{"name"}}},
		Properties:   map[string]any{"k": "v"},
		Timestamps:   covT_bool(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`app.Entity("user"`, `Table: "users"`, "Relations:", "SoftDelete: true",
		"MultiTenant: true", "CRUD: boolPtr(true)", "MCP: true", `CursorField: "id"`,
		`CursorFields: []string{"a", "b"}`, "Indices:", "Properties:", ".WithTimestamps(true)"} {
		if !strings.Contains(got, want) {
			t.Errorf("registration missing %q:\n%s", want, got)
		}
	}
}

func TestRenderEntityRegistrationFieldError(t *testing.T) {
	_, err := renderEntityRegistration(framework.EntityDeclaration{
		Name: "x", Fields: []framework.FieldDeclaration{{Name: "f", Type: "bogus"}},
	})
	if err == nil {
		t.Fatal("expected field error")
	}
	_, err = renderEntityRegistration(framework.EntityDeclaration{
		Name: "x", Properties: map[string]any{"k": struct{}{}},
	})
	if err == nil {
		t.Fatal("expected properties error")
	}
}

func TestRenderEntityModelRelations(t *testing.T) {
	got := renderEntityModel(framework.EntityDeclaration{
		Name: "user",
		Fields: []framework.FieldDeclaration{
			{Name: "id", Type: "string"}, {Name: "age", Type: "int"},
		},
		Relations: []framework.Relation{
			{Type: framework.RelHasOne, Name: "profile", Entity: "profile"},
			{Type: framework.RelHasMany, Name: "posts", Entity: "post"},
		},
	})
	for _, want := range []string{"type User struct", "ID string", "Age int", "Profile *Profile", "Posts []*Post"} {
		if !strings.Contains(got, want) {
			t.Errorf("model missing %q:\n%s", want, got)
		}
	}
}

func TestValidateOutputDir(t *testing.T) {
	bad := []string{"", " ", "/abs", ".", "..", "../b"}
	for _, d := range bad {
		if err := validateOutputDir(d); err == nil {
			t.Errorf("validateOutputDir(%q) = nil, want error", d)
		}
	}
	if err := validateOutputDir(".gofastr/entities"); err != nil {
		t.Errorf("valid dir rejected: %v", err)
	}
}

func TestSafeCleanOutputDir(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Non-existent dir → nil.
	if err := safeCleanOutputDir(filepath.Join(base, "nope")); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	// Owned files get removed.
	dir := base
	sub := filepath.Join(dir, "out")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"register.go", "models.go", ".gitkeep"} {
		if err := os.WriteFile(filepath.Join(sub, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := safeCleanOutputDir(sub); err != nil {
		t.Fatalf("clean owned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, "register.go")); !os.IsNotExist(err) {
		t.Fatal("register.go not removed")
	}
	// Unknown file → refuse.
	if err := os.WriteFile(filepath.Join(sub, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := safeCleanOutputDir(sub); err == nil {
		t.Fatal("expected refusal for unknown entry")
	}
	// Path is a file, not a dir.
	fpath := filepath.Join(dir, "afile")
	if err := os.WriteFile(fpath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := safeCleanOutputDir(fpath); err == nil {
		t.Fatal("expected not-a-directory error")
	}
}

func TestPrintJSONHelpers(t *testing.T) {
	out := covT_capStdout(t, func() {
		printGeneratedFilesJSON([]generatedFile{{name: "a.go", content: "xy"}, {name: "b.go", content: "z"}})
	})
	if !strings.Contains(out, `"path":"a.go"`) || !strings.Contains(out, `"size":2`) {
		t.Fatalf("files json: %s", out)
	}
	out = covT_capStdout(t, func() {
		printGeneratedErrorsJSON(errString("boom"), errString("bad"))
	})
	if !strings.Contains(out, `"message":"boom"`) {
		t.Fatalf("errors json: %s", out)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestFieldTypeConstAll(t *testing.T) {
	cases := map[schema.FieldType]string{
		schema.String: "String", schema.Int: "Int", schema.Float: "Float",
		schema.Bool: "Bool", schema.Enum: "Enum", schema.Date: "String",
	}
	for in, want := range cases {
		if got := fieldTypeConst(in); got != want {
			t.Errorf("fieldTypeConst(%v)=%q want %q", in, got, want)
		}
	}
}

func TestGenerateEntityWritesFile(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() {
		generateEntity([]string{"Product", "title:string:required", "price:float", "active:bool", "qty:int", "kind:enum", "made:date", "weird:zzz"})
	})
	data, err := os.ReadFile(filepath.Join(dir, "entities", "product.go"))
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	s := string(data)
	for _, want := range []string{"package entities", "registerProduct", `Table: "product"`, "schema.String", "schema.Float", "schema.Bool", "schema.Int", "schema.Enum", "Required: true"} {
		if !strings.Contains(s, want) {
			t.Errorf("generated entity missing %q", want)
		}
	}
}

func TestGenerateEntityDefaultsField(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { generateEntity([]string{"Tag"}) })
	if _, err := os.Stat(filepath.Join(dir, "entities", "tag.go")); err != nil {
		t.Fatalf("default-field entity not written: %v", err)
	}
}

func TestGenerateEntityNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { generateEntity(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestGenerateEntityBadFieldExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { generateEntity([]string{"X", "nocolon"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestGenerateEntityExistingFileExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { generateEntity([]string{"Dup", "name:string"}) })
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { generateEntity([]string{"Dup", "name:string"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}
