package appgen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// A build re-runs the codegen config, so a scaffold generator listed there
// would regenerate code the user owns. Every scaffold entry is refused and
// the error names the direct command.
func TestConfigRefusesScaffoldGenerators(t *testing.T) {
	for _, g := range Generators {
		cfg := codegen.CodegenConfig{Generators: []codegen.GeneratorConfig{{Name: "sdk"}, {Name: g.Name}}}
		err := checkConfigGenerators(cfg)
		if g.Kind == DerivedKind {
			if err != nil {
				t.Errorf("derived %q refused: %v", g.Name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("scaffold %q accepted in the codegen config", g.Name)
			continue
		}
		for _, want := range []string{"codegen.generators[1]", "gofastr generate " + g.Name, "scaffold"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q error %q missing %q", g.Name, err, want)
			}
		}
	}
}

// An extension may share a built-in's name: the entry runs the extension,
// not the scaffold generator, so it is not refused.
func TestConfigAllowsExtensionNamedLikeScaffold(t *testing.T) {
	cfg := codegen.CodegenConfig{Generators: []codegen.GeneratorConfig{{Name: "app", Extension: "my-app-gen"}}}
	if err := checkConfigGenerators(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestStylesEntriesSplitFromEngine(t *testing.T) {
	gens := []codegen.GeneratorConfig{
		{Name: "sdk"},
		{Name: "styles", Config: map[string]any{"patterns": []any{"./ui/...", "./site"}}},
		{Name: "custom/reports", Extension: "reports", Output: "reports"},
		{Name: "styles"},
		{Name: "styles", Extension: "my-styles"},
	}
	engine, patterns, err := splitStylesEntries(gens)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range engine {
		names = append(names, g.Name+"/"+g.Extension)
	}
	if want := []string{"sdk/", "custom/reports/reports", "styles/my-styles"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("engine = %v, want %v", names, want)
	}
	if want := [][]string{{"./ui/...", "./site"}, {}}; !reflect.DeepEqual(patterns, want) {
		t.Fatalf("patterns = %#v, want %#v", patterns, want)
	}

	_, patterns, err = splitStylesEntries([]codegen.GeneratorConfig{{Name: "sdk"}})
	if err != nil || patterns != nil {
		t.Fatalf("no styles entry: patterns = %#v, err = %v", patterns, err)
	}
}

func TestStylesEntryRejectsBadConfig(t *testing.T) {
	for name, gen := range map[string]codegen.GeneratorConfig{
		"output":      {Name: "styles", Output: "gen"},
		"source":      {Name: "styles", Source: codegen.SourceConfig{Type: "json_file"}},
		"unknown key": {Name: "styles", Config: map[string]any{"pattern": []any{"./..."}}},
		"not a list":  {Name: "styles", Config: map[string]any{"patterns": "./..."}},
		"not strings": {Name: "styles", Config: map[string]any{"patterns": []any{3}}},
		"blank":       {Name: "styles", Config: map[string]any{"patterns": []any{" "}}},
		"flag":        {Name: "styles", Config: map[string]any{"patterns": []any{"--help"}}},
	} {
		if _, _, err := splitStylesEntries([]codegen.GeneratorConfig{gen}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGenerateConfigScaffoldExits(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	writeCodegenConfig(t, dir, "    - name: app\n")
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { GenerateProject(nil) })
	})
	if code != 1 || !strings.Contains(out, "`gofastr generate app`") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestGenerateStylesOnlyDryRun(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	writeCodegenConfig(t, dir, "    - name: styles\n")
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { GenerateProject([]string{"--dry-run"}) })
	})
	if code != -1 || !strings.Contains(out, "styles has no dry run") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	out = clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { GenerateProject([]string{"--dry-run", "--json"}) })
	})
	if code != 1 || !strings.Contains(out, "no dry run") {
		t.Fatalf("--json: exit %d, output:\n%s", code, out)
	}
}

func writeCodegenConfig(t *testing.T, dir, generatorsYAML string) {
	t.Helper()
	body := "version: 1\ncodegen:\n  output: gen\n  generators:\n" + generatorsYAML
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
