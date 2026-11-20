package newcmd

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `gofastr new handler type` wrote `func type(...)`, which does not parse;
// `main` and `init` redeclared the package's entry points, and a
// predeclared name (`string`, `len`) shadowed it for every other file.
// Each passed the identifier guard and broke the build.
func TestScaffoldHandlerRefusesNamesThatBreakTheBuild(t *testing.T) {
	for _, name := range []string{"type", "func", "range", "main", "init", "string", "len", "error", "_"} {
		dir := t.TempDir()
		if err := scaffoldHandler(dir, name, "GET", "/x", false); err == nil {
			t.Errorf("scaffoldHandler(%q) = nil, want a refusal", name)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("scaffoldHandler(%q) wrote %d file(s) before refusing", name, len(entries))
		}
	}
}

// A name the package already declares (HomeScreen in an init scaffold's
// screens.go) is a redeclaration.
func TestScaffoldHandlerRefusesDeclaredName(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "screens.go", "package main\n\nfunc HomeScreen() {}\n")
	if err := scaffoldHandler(dir, "HomeScreen", "GET", "/", false); err == nil || !strings.Contains(err.Error(), "already declares") {
		t.Fatalf("scaffoldHandler(HomeScreen) = %v, want an already-declares refusal", err)
	}
}

func TestScaffoldHandlerRefusesImportedName(t *testing.T) {
	for _, existing := range []bool{false, true} {
		dir := t.TempDir()
		if existing {
			writeGo(t, dir, "main.go", "package main\n\nfunc main() {}\n")
		}
		err := scaffoldHandler(dir, "http", "GET", "/http", false)
		if err == nil || !strings.Contains(err.Error(), "import") {
			t.Errorf("scaffoldHandler(http), existing package %t = %v, want an import-name refusal", existing, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "http_handler.go")); !os.IsNotExist(err) {
			t.Errorf("scaffoldHandler(http) wrote a file before refusing: %v", err)
		}
	}
}

func TestScaffoldHandlerPackageImports(t *testing.T) {
	for _, tc := range []struct {
		name, imports, want string
	}{
		{"log", `"log"`, `conflicts with the import of "log" in main.go`},
		{"logger", `logger "log"`, `conflicts with the import of "log" in main.go`},
		{"log", `logger "log"`, ""},
		{"log", `_ "log"`, ""},
		{"log", `. "log"`, ""},
		{"Log", `"log"`, ""},
		{"dep", `"example.com/dep/v2"`, `conflicts with the import of "example.com/dep/v2" in main.go`},
		{"yaml", `"gopkg.in/yaml.v3"`, `conflicts with the import of "gopkg.in/yaml.v3" in main.go`},
		{"pgx", `"github.com/jackc/pgx/v5"`, `conflicts with the import of "github.com/jackc/pgx/v5" in main.go`},
		{"v2", `"example.com/dep/v2"`, ""},
	} {
		t.Run(tc.name+tc.imports, func(t *testing.T) {
			dir := t.TempDir()
			writeGo(t, dir, "main.go", "package main\nimport "+tc.imports+"\n")
			err := scaffoldHandler(dir, tc.name, "GET", "/x", false)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("scaffoldHandler(%q) = %v, want %q", tc.name, err, tc.want)
				}
				if _, err := os.Stat(filepath.Join(dir, strings.ToLower(tc.name)+"_handler.go")); !os.IsNotExist(err) {
					t.Errorf("handler written before refusal: %v", err)
				}
			}
		})
	}
}

func TestImportNamesAssumedNames(t *testing.T) {
	for _, tc := range []struct{ importPath, want, not string }{
		{"example.com/dep/v2", "dep", "v2"},
		{"gopkg.in/yaml.v3", "yaml", "yaml.v3"},
		{"github.com/jackc/pgx/v5", "pgx", "v5"},
		{"example.com/go-foo-bar", "foo", "go-foo-bar"},
		{"example.com/vx", "vx", "v"},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "", "package main\nimport "+strconv.Quote(tc.importPath), parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		names := importNames(f, "main.go", nil)
		if names[tc.want] == "" || names[tc.not] != "" {
			t.Errorf("importNames(%q) = %v, want %q, not %q", tc.importPath, names, tc.want, tc.not)
		}
	}
}

func TestScaffoldHandlerDeclaredImportName(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "go.mod", "module example.com/app\n\ngo 1.27.4\n")
	if err := os.Mkdir(filepath.Join(dir, "gopher-lua"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeGo(t, filepath.Join(dir, "gopher-lua"), "lua.go", "package lua\n")
	writeGo(t, dir, "main.go", "package main\nimport \"example.com/app/gopher-lua\"\n")
	clitest.CovT_chdir(t, dir)
	if err := scaffoldHandler(dir, "lua", "GET", "/", false); err == nil || !strings.Contains(err.Error(), `import of "example.com/app/gopher-lua" in main.go`) {
		t.Errorf("lua: %v, want declared-name import refusal", err)
	}
	if err := scaffoldHandler(dir, "gopher", "GET", "/", false); err != nil {
		t.Errorf("gopher: %v, want success", err)
	}
	if err := checkRouteHandler("lua"); err == nil {
		t.Error("route lua: want import refusal")
	}
	if err := checkRouteHandler("gopher"); err != nil {
		t.Errorf("route gopher: %v", err)
	}
}

func TestScaffoldHandlerTestPackageScope(t *testing.T) {
	for _, tc := range []struct{ pkg, name, want string }{
		{"main", "log", `import of "log" in a_test.go`},
		{"main", "helper", "already declares helper"},
		{"main_test", "log", ""},
		{"main_test", "helper", ""},
	} {
		t.Run(tc.pkg+tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGo(t, dir, "z.go", "package main\n")
			writeGo(t, dir, "a_test.go", "package "+tc.pkg+"\nimport \"log\"\nfunc helper() {}\n")
			clitest.CovT_chdir(t, dir)
			if tc.name == "log" {
				err := checkRouteHandler("log")
				if (err != nil) != (tc.want != "") {
					t.Errorf("route log: %v, want refusal %t", err, tc.want != "")
				}
			}
			err := scaffoldHandler(dir, tc.name, "GET", "/", false)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestScaffoldHandlerDotlessModuleImportName(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "go.mod", "module local/app\n\ngo 1.23\n")
	for _, subdir := range []string{"gopher-lua", "handlers"} {
		if err := os.Mkdir(filepath.Join(root, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeGo(t, filepath.Join(root, "gopher-lua"), "lua.go", "package lua\n")
	dir := filepath.Join(root, "handlers")
	writeGo(t, dir, "handlers.go", "package handlers\nimport \"local/app/gopher-lua\"\n")
	clitest.CovT_chdir(t, dir)
	if err := scaffoldHandler(dir, "lua", "GET", "/", false); err == nil || !strings.Contains(err.Error(), `import of "local/app/gopher-lua" in handlers.go`) {
		t.Errorf("lua: %v, want declared-name import refusal", err)
	}
	if err := scaffoldHandler(dir, "gopher", "GET", "/", false); err != nil {
		t.Errorf("gopher: %v, want success", err)
	}
	if err := checkRouteHandler("lua"); err == nil {
		t.Error("route lua: want import refusal")
	}
	if err := checkRouteHandler("gopher"); err != nil {
		t.Errorf("route gopher: %v", err)
	}
}

func TestScaffoldHandlerImportResolutionFallback(t *testing.T) {
	for _, missingGo := range []bool{false, true} {
		t.Run(strconv.FormatBool(missingGo), func(t *testing.T) {
			dir := t.TempDir()
			writeGo(t, dir, "go.mod", "module example.com/app\n\ngo 1.27.4\n")
			writeGo(t, dir, "main.go", "package main\nimport \"example.invalid/go-missing/v2\"\n")
			t.Setenv("GOPROXY", "off")
			if missingGo {
				t.Setenv("PATH", dir)
			}
			if err := scaffoldHandler(dir, "missing", "GET", "/", false); err == nil || !strings.Contains(err.Error(), `import of "example.invalid/go-missing/v2"`) {
				t.Errorf("got %v, want heuristic refusal", err)
			}
			if err := scaffoldHandler(dir, "Available", "GET", "/", false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestScaffoldHandlerPreservesModuleFiles(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	for _, hasSum := range []bool{false, true} {
		t.Run(strconv.FormatBool(hasSum), func(t *testing.T) {
			dir := t.TempDir()
			mod := "module example.com/app\n\ngo 1.18\n\nreplace example.com/dep => ./dep\n"
			writeGo(t, dir, "go.mod", mod)
			if err := os.Mkdir(filepath.Join(dir, "dep"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeGo(t, filepath.Join(dir, "dep"), "go.mod", "module example.com/dep\n\ngo 1.23\n")
			writeGo(t, filepath.Join(dir, "dep"), "dep.go", "package dep\n")
			writeGo(t, dir, "main.go", "package main\nimport \"example.com/dep\"\n")
			sum := "example.com/unused v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
			if hasSum {
				writeGo(t, dir, "go.sum", sum)
			}
			if err := scaffoldHandler(dir, "Available", "GET", "/", false); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(dir, "go.mod")); err != nil || string(got) != mod {
				t.Errorf("go.mod changed: %v\n%s", err, got)
			}
			got, err := os.ReadFile(filepath.Join(dir, "go.sum"))
			if hasSum {
				if err != nil || string(got) != sum {
					t.Errorf("go.sum changed: %v\n%s", err, got)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("go.sum created: %v\n%s", err, got)
			}
		})
	}
}

func TestResolveImportNamesSkipsStandardLibrary(t *testing.T) {
	for _, tc := range []struct{ imports, want string }{
		{"", ""},
		{"\"net/http\"; \"log\"", ""},
		{"\"net/http\"; alias \"example.com/aliased\"", ""},
		{"\"net/http\"; \"example.com/dep\"; \"log\"", "example.com/dep"},
		{"\"local/app\"", "local/app"},
		{"\"local/app/gopher-lua\"", "local/app/gopher-lua"},
		{"\"local/application/dep\"", ""},
	} {
		t.Run(tc.imports, func(t *testing.T) {
			dir := t.TempDir()
			writeGo(t, dir, "go.mod", "module local/app\n\ngo 1.23\n")
			original := importNameCommand
			t.Cleanup(func() { importNameCommand = original })
			calls := 0
			importNameCommand = func(name string, args ...string) *exec.Cmd {
				calls++
				if name != "go" || len(args) < 7 || strings.Join(args[6:], " ") != tc.want {
					t.Errorf("unexpected command: %s %q", name, args)
				}
				return exec.Command(filepath.Join(t.TempDir(), "missing-go"))
			}
			f, err := parser.ParseFile(token.NewFileSet(), "", "package main\nimport ("+tc.imports+")", parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			resolveImportNames(dir, []*ast.File{f})
			wantCalls := 0
			if tc.want != "" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("command calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

// Run from a non-main package directory (entities/), the handler was
// written as package main beside package entities: "found packages
// entities and main". It takes the directory's package, and the file is
// gofmt-clean.
func TestScaffoldHandlerUsesTheDirectorysPackage(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "entities.go", "package entities\n")
	writeGo(t, dir, "entities_test.go", "package entities_test\n")
	if err := scaffoldHandler(dir, "Orders", "GET", "/orders", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "orders_handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\npackage entities\n") {
		t.Errorf("handler does not take the directory's package:\n%s", got)
	}
	formatted, err := format.Source(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != string(got) {
		t.Errorf("scaffolded handler is not gofmt-clean:\n%s", got)
	}

	empty := t.TempDir()
	if err := scaffoldHandler(empty, "Orders", "GET", "/orders", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(empty, "orders_handler.go")); !strings.Contains(string(got), "\npackage main\n") {
		t.Errorf("handler in a directory with no Go files is not package main:\n%s", got)
	}
}

func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}
