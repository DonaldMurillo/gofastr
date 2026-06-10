package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

// ── build.go ──────────────────────────────────────────────────────────

func covT_tinyModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module buildtest\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, dir)
	return dir
}

func TestRunBuildNoGenerate(t *testing.T) {
	dir := covT_tinyModule(t)
	covT_capStdout(t, func() { runBuild([]string{"--no-generate", "-o=" + filepath.Join(dir, "out", "server")}) })
	if _, err := os.Stat(filepath.Join(dir, "out", "server")); err != nil {
		t.Fatalf("binary not built: %v", err)
	}
}

func TestRunBuildOutputFlag(t *testing.T) {
	dir := covT_tinyModule(t)
	covT_capStdout(t, func() { runBuild([]string{"--no-generate", "--output=" + filepath.Join(dir, "bin2")}) })
	if _, err := os.Stat(filepath.Join(dir, "bin2")); err != nil {
		t.Fatalf("binary not built: %v", err)
	}
}

// ── audit.go runAudit ─────────────────────────────────────────────────

func TestRunAuditUsageExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAudit(nil) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAuditUnknownExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAudit([]string{"frob"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAuditDeps(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module audittest\n\ngo 1.21\n")
	mustWrite(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	out := covT_capStdout(t, func() { runAudit([]string{"deps", dir}) })
	_ = out // formatAuditReport always prints something
}

func TestRunAuditLintClean(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module linttest\n\ngo 1.21\n")
	mustWrite(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	out := covT_capStdout(t, func() { runAudit([]string{"lint", dir}) })
	if !strings.Contains(out, "No findings") {
		t.Fatalf("clean lint should report no findings: %s", out)
	}
}

func TestRunAuditLintFindingExits(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module linttest\n\ngo 1.21\n")
	// A t.Skip in a test file triggers the ruleTestSkip finding.
	mustWrite(t, dir, "x_test.go", "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skip(\"todo\")\n}\n")
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runAudit([]string{"lint", dir}) })
	})
	if code != 1 {
		t.Fatalf("findings should exit 1, got %d", code)
	}
}

func TestReadModulePathAndImportPathFor(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/proj\n\ngo 1.21\n")
	if readModulePath(dir) != "example.com/proj" {
		t.Fatalf("module = %q", readModulePath(dir))
	}
	if readModulePath(t.TempDir()) != "" {
		t.Fatal("missing go.mod → empty")
	}
	if got := importPathFor("example.com/proj", dir, dir); got != "example.com/proj" {
		t.Fatalf("root import = %q", got)
	}
	sub := filepath.Join(dir, "pkg")
	if got := importPathFor("example.com/proj", dir, sub); got != "example.com/proj/pkg" {
		t.Fatalf("sub import = %q", got)
	}
}

// ── generate_typed.go columnConstructor ───────────────────────────────

func TestColumnConstructorAll(t *testing.T) {
	cases := map[string]string{
		"int": "framework.NewIntColumn", "integer": "framework.NewIntColumn",
		"float": "framework.NewFloatColumn", "decimal": "framework.NewFloatColumn",
		"bool": "framework.NewBoolColumn", "timestamp": "framework.NewTimestampColumn",
		"date": "framework.NewTimestampColumn", "uuid": "framework.NewUUIDColumn",
		"relation": "framework.NewUUIDColumn", "string": "framework.NewStringColumn",
		"json": "framework.NewStringColumn",
	}
	for in, want := range cases {
		if got := columnConstructor(in); got != want {
			t.Errorf("columnConstructor(%q)=%q want %q", in, got, want)
		}
	}
}

// ── blueprint.go scalar/value helpers ─────────────────────────────────

func sc(v any) *coreyaml.Node { return &coreyaml.Node{Kind: coreyaml.Scalar, Value: v} }
func lst(n ...*coreyaml.Node) *coreyaml.Node {
	return &coreyaml.Node{Kind: coreyaml.List, List: n}
}
func mp(m map[string]*coreyaml.Node) *coreyaml.Node {
	return &coreyaml.Node{Kind: coreyaml.Map, Map: m}
}

func TestBlueprintScalarHelpers(t *testing.T) {
	// boolValue
	if !boolValue(sc(true)) || boolValue(sc("false")) {
		t.Fatal("boolValue bool")
	}
	if !boolValue(sc("TRUE")) {
		t.Fatal("boolValue string true")
	}
	if boolValue(nil) || boolValue(lst()) {
		t.Fatal("boolValue non-scalar")
	}
	// intValue
	if intValue(sc(int64(5))) != 5 || intValue(sc(float64(3.9))) != 3 {
		t.Fatal("intValue numeric")
	}
	if intValue(sc("x")) != 0 || intValue(nil) != 0 {
		t.Fatal("intValue non-numeric")
	}
	// floatValue
	if floatValue(sc(int64(2))) != 2 || floatValue(sc(float64(1.5))) != 1.5 {
		t.Fatal("floatValue numeric")
	}
	if floatValue(sc("x")) != 0 || floatValue(nil) != 0 {
		t.Fatal("floatValue non-numeric")
	}
	// scalarValue
	if scalarValue(sc("hi")) != "hi" || scalarValue(lst()) != nil {
		t.Fatal("scalarValue")
	}
	// anyValue
	if anyValue(nil) != nil {
		t.Fatal("anyValue nil")
	}
	if anyValue(sc("x")) != "x" {
		t.Fatal("anyValue scalar")
	}
	if l, ok := anyValue(lst(sc("a"), sc("b"))).([]any); !ok || len(l) != 2 {
		t.Fatal("anyValue list")
	}
	if m, ok := anyValue(mp(map[string]*coreyaml.Node{"k": sc("v")})).(map[string]any); !ok || m["k"] != "v" {
		t.Fatal("anyValue map")
	}
}

func TestExpectMapAndList(t *testing.T) {
	if _, err := expectMap(nil, "x"); err == nil {
		t.Fatal("nil map should error")
	}
	if _, err := expectMap(lst(), "x"); err == nil {
		t.Fatal("list-as-map should error")
	}
	if _, err := expectMap(mp(nil), "x"); err != nil {
		t.Fatalf("valid map: %v", err)
	}
	if l, err := expectList(nil, "x"); err != nil || l != nil {
		t.Fatal("nil list → nil,nil")
	}
	if _, err := expectList(sc("x"), "y"); err == nil {
		t.Fatal("scalar-as-list should error")
	}
	if _, err := expectList(lst(sc("a")), "y"); err != nil {
		t.Fatalf("valid list: %v", err)
	}
}

func TestRelationTypeFromStringAndScreenType(t *testing.T) {
	good := map[string]framework.RelationType{
		"": framework.RelManyToOne, "belongs_to": framework.RelManyToOne,
		"has_one": framework.RelHasOne, "has_many": framework.RelHasMany,
		"many_to_many": framework.RelManyToMany,
	}
	for in, want := range good {
		got, err := relationTypeFromString(in)
		if err != nil || got != want {
			t.Errorf("relationTypeFromString(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := relationTypeFromString("bogus"); err == nil {
		t.Fatal("bad relation type")
	}

	st := map[string]string{
		"": "app.ScreenPage", "page": "app.ScreenPage", "drawer": "app.ScreenDrawer",
		"sheet": "app.ScreenSheet", "dialog": "app.ScreenDialog", "modal": "app.ScreenDialog",
	}
	for in, want := range st {
		got, err := screenTypeConst(in)
		if err != nil || got != want {
			t.Errorf("screenTypeConst(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := screenTypeConst("bogus"); err == nil {
		t.Fatal("bad screen type")
	}
}

func TestBlueprintThemeColorPathAll(t *testing.T) {
	keys := []string{"primary", "primary-fg", "secondary", "background", "surface",
		"surface-soft", "text", "text-muted", "text-subtle", "border", "border-strong",
		"accent", "success", "warning", "danger", "info"}
	for _, k := range keys {
		if _, ok := blueprintThemeColorPath(k); !ok {
			t.Errorf("blueprintThemeColorPath(%q) not ok", k)
		}
	}
	if _, ok := blueprintThemeColorPath("nonsense"); ok {
		t.Fatal("unknown key should be !ok")
	}
}
