package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func covT_f64(v float64) *float64 { return &v }

func covT_bool(v bool) *bool { return &v }

func TestRelationTypeConstAll(t *testing.T) {
	cases := map[framework.RelationType]string{
		framework.RelHasOne:        "RelHasOne",
		framework.RelHasMany:       "RelHasMany",
		framework.RelManyToOne:     "RelManyToOne",
		framework.RelManyToMany:    "RelManyToMany",
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
		"bool": "bool", "boolean": "bool", "json": "any", "string": "string", "x": "string",
	}
	for in, want := range cases {
		if got := goTypeForField(in); got != want {
			t.Errorf("goTypeForField(%q)=%q want %q", in, got, want)
		}
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
		// A number decoded with UseNumber keeps every digit of an int64,
		// which a float64 round trip would round (2^53 + 1).
		{json.Number("9007199254740993"), "9007199254740993"},
		{json.Number("2.5"), "2.5"},
		{map[string]any{"n": json.Number("-4")}, `map[string]any{"n": -4}`},
	}
	if _, err := renderGoLiteral(json.Number("12x")); err == nil {
		t.Error("a malformed json.Number rendered")
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
	// Expression is the key for a function index and replaces Columns. Dropping
	// it emitted an index with no key at all, so a declared UNIQUE constraint
	// silently did not exist in the generated app, and a case-insensitive
	// unique index is the usual reason to write one.
	got = renderIndexLiteral(framework.Index{Name: "uniq_email_lower", Expression: "LOWER(email)", Unique: true})
	for _, want := range []string{`Name: "uniq_email_lower"`, `Expression: "LOWER(email)"`, "Unique: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("index literal missing %q: %s", want, got)
		}
	}
}

func TestRenderRelationLiteralFull(t *testing.T) {
	got := renderRelationLiteral(framework.Relation{
		Type: framework.RelManyToMany, Name: "tags", Entity: "tag",
		ForeignKey: []string{"fk"}, Through: "jt", LocalKey: "lk", ForeignKeyTarget: "fkt",
	})
	for _, want := range []string{"RelManyToMany", `Name: "tags"`, `Entity: "tag"`, `ForeignKey: []string{"fk"}`, `Through: "jt"`, `LocalKey: "lk"`, `ForeignKeyTarget: "fkt"`} {
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
	got, err := RenderEntityRegistration(framework.EntityDeclaration{
		Name: "user", Table: "users",
		Fields:    []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		Relations: []framework.Relation{{Type: framework.RelHasMany, Name: "posts", Entity: "post"}}, Scope: &framework.ScopeDeclaration{SoftDelete: true, MultiTenant: true}, Exposure: &framework.ExposureDeclaration{CRUD: covT_bool(true), MCP: true}, Pagination: &framework.PaginationDeclaration{CursorField: "id", CursorFields: []string{"a", "b"}}, Indices: []framework.Index{{Name: "ix", Columns: []string{"name"}}},
		Properties: map[string]any{"k": "v"},
		Timestamps: covT_bool(true),
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
	_, err := RenderEntityRegistration(framework.EntityDeclaration{
		Name: "x", Fields: []framework.FieldDeclaration{{Name: "f", Type: "bogus"}},
	})
	if err == nil {
		t.Fatal("expected field error")
	}
	_, err = RenderEntityRegistration(framework.EntityDeclaration{
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
	bad := []string{"", " ", t.TempDir(), ".", "..", "../b"}
	for _, d := range bad {
		if err := validateOutputDir(d); err == nil {
			t.Errorf("validateOutputDir(%q) = nil, want error", d)
		}
	}
	if err := validateOutputDir("gen/entities"); err != nil {
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
	// main.go is part of the app generator's output root; the cleaner
	// must own it so re-running `gofastr generate app` over an existing
	// gen/ succeeds.
	for _, n := range []string{"main.go", "register.go", "models.go", ".gitkeep"} {
		if err := os.WriteFile(filepath.Join(sub, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := safeCleanOutputDir(sub); err != nil {
		t.Fatalf("clean owned: %v", err)
	}
	for _, n := range []string{"main.go", "register.go"} {
		if _, err := os.Stat(filepath.Join(sub, n)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", n)
		}
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
	out := clitest.CovT_capStdout(t, func() {
		printGeneratedFilesJSON([]GeneratedFile{{Name: "a.go", Content: "xy"}, {Name: "b.go", Content: "z"}})
	})
	if !strings.Contains(out, `"path":"a.go"`) || !strings.Contains(out, `"size":2`) {
		t.Fatalf("files json: %s", out)
	}
	out = clitest.CovT_capStdout(t, func() {
		printGeneratedErrorsJSON(errString("boom"), errString("bad"))
	})
	if !strings.Contains(out, `"message":"boom"`) {
		t.Fatalf("errors json: %s", out)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
