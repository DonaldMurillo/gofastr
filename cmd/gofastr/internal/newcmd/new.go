package newcmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// validateScaffoldName rejects names that would escape the project root.
// Scaffolding commands accept a short identifier (e.g. "User", "Post") and
// must never accept path traversal, absolute paths, or directory separators.
func validateScaffoldName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is not a valid identifier", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q must not contain path separators", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name %q must not contain '..'", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("name %q must be a bare identifier", name)
	}
	return nil
}

// newUsage prints the resource list for `gofastr new`.
func newUsage() {
	fmt.Println("Usage: gofastr new <resource> <name> [flags]")
	fmt.Println("Resources:")
	fmt.Println("  handler  <Name> [--method=GET] [--path=/x] [-overwrite]   Scaffold an HTTP handler")
	fmt.Println("  route    <path>  [--method=GET] [--handler=Name]   Print a route registration snippet")
	fmt.Println("Flags:")
	fmt.Println("  -overwrite   Rewrite the target file if it already exists")
	fmt.Println("  -h, --help   Show this help")
}

// RunNew handles the `gofastr new` subcommand, a lower-level scaffolding
// alternative to kiln's visual builder.
func RunNew(args []string) {
	// `gofastr new -h` / `gofastr new --help` exits 0 with help.
	for _, a := range args {
		if a == "-h" || a == "--help" {
			newUsage()
			return
		}
	}

	if len(args) == 0 {
		newUsage()
		cli.OsExit(1)
	}

	resource := args[0]
	rest := args[1:]

	// Strip -overwrite from rest, leaving the resource-specific args.
	overwrite, rest := extractOverwriteFlag(rest)

	switch resource {
	case "entity":
		cli.Fail("`gofastr new entity` has been removed.")
		cli.Info("Declare entities in a gofastr.yml app spec instead. See `gofastr docs app-spec`.")
		cli.OsExit(1)
	case "handler":
		runNewHandler(rest, overwrite)
	case "route":
		runNewRoute(rest)
	default:
		cli.Fail("Unknown resource: %s", resource)
		cli.Info("Supported: handler, route")
		cli.OsExit(1)
	}
}

// extractOverwriteFlag scans args for -overwrite / --overwrite and returns
// (true, argsWithoutFlag) if present.
func extractOverwriteFlag(args []string) (bool, []string) {
	out := args[:0:0]
	overwrite := false
	for _, a := range args {
		if a == "-overwrite" || a == "--overwrite" {
			overwrite = true
			continue
		}
		out = append(out, a)
	}
	return overwrite, out
}

// runNewHandler is the CLI wrapper around scaffoldHandler.
func runNewHandler(args []string, overwrite bool) {
	if len(args) == 0 {
		cli.Fail("Usage: gofastr new handler <Name> --method <GET|POST> --path <path>")
		cli.OsExit(1)
	}

	name := args[0]
	method := "GET"
	path := "/" + strings.ToLower(name)
	// Both --method POST and --method=POST: the usage line shows the
	// space form, which this loop once ignored, scaffolding GET /<name>.
	if _, err := cli.ParseFlags(args[1:], map[string]*string{"--method": &method, "--path": &path}, nil); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	method = strings.ToUpper(method)
	if err := checkRouteMethod(method); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	if err := scaffoldHandler(".", name, method, path, overwrite); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	cli.Success("Scaffolded handler %q (%s %s)", name, method, path)
}

// runNewRoute is the CLI wrapper that prints a route registration snippet.
func runNewRoute(args []string) {
	if len(args) == 0 {
		cli.Fail("Usage: gofastr new route <path> --method <GET|POST> --handler <name>")
		cli.OsExit(1)
	}

	path := args[0]
	method := "GET"
	handler := "handler"
	if _, err := cli.ParseFlags(args[1:], map[string]*string{"--method": &method, "--handler": &handler}, nil); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	method = strings.ToUpper(method)
	if err := checkRouteMethod(method); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	// The handler lands at an expression slot of the printed Go line: a
	// name that is not an identifier, or a selector of them (h.List),
	// prints a line that does not compile.
	if err := checkRouteHandler(handler); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	cli.Info("Add this to your app setup:")
	cli.Printf("\n  %s\n\n", routeSnippet(method, path, handler))
}

// routeMethods are the methods new handler and new route accept, upper
// case: the HTTP methods a route serves.
var routeMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"}

// checkRouteMethod refuses a method outside routeMethods.
func checkRouteMethod(method string) error {
	if !slices.Contains(routeMethods, method) {
		return fmt.Errorf("invalid method %q: use one of %s", method, strings.Join(routeMethods, ", "))
	}
	return nil
}

// checkRouteHandler refuses a --handler that is not a Go identifier or a
// selector chain of them (h.List, handlers.Things). A bare name gets new
// handler's guard too: a keyword, main, init, _ or a predeclared name is
// no handler.
func checkRouteHandler(handler string) error {
	parts := strings.Split(handler, ".")
	for _, p := range parts {
		if !project.IsGoIdentifier(p) || token.IsKeyword(p) || p == "_" {
			return fmt.Errorf("invalid handler %q: must be a Go identifier or a selector such as h.List", handler)
		}
	}
	if len(parts) == 1 {
		if reason := unusableFuncName(handler); reason != "" {
			return fmt.Errorf("invalid handler %q: %s; pick another name", handler, reason)
		}
	}
	return nil
}

// routeSnippet returns the canonical app.Router().Handle(...) line for
// the given route. Pure helper; used by both runNewRoute and golden tests.
func routeSnippet(method, path, handler string) string {
	return fmt.Sprintf("app.Router().Handle(%q, %q, %s)", method, path, handler)
}

// scaffoldHandler writes an HTTP handler file at <baseDir>/<name>_handler.go.
// Returns an error when the file already exists and overwrite is false.
func scaffoldHandler(baseDir, rawName, method, path string, overwrite bool) error {
	if err := validateScaffoldName(rawName); err != nil {
		return fmt.Errorf("invalid handler name: %w", err)
	}
	// The name lands in `func %s(...)` at identifier position and in the
	// file name + leading comment: it must be a bare Go identifier, the
	// same guard every generate-side sibling carries
	// (TestScaffoldNameGuardedByValidate, TestHookHandlerMustDeriveIdentifier).
	if !project.IsGoIdentifier(rawName) {
		return fmt.Errorf("invalid handler name %q: must be a Go identifier (letters, digits, underscores, starting with a letter)", rawName)
	}
	// method+path ride the `// %s handles %s %s.` comment slot. A newline
	// or control/bidi byte closes the comment and lands the rest at
	// declaration position (a smuggled init() compiled and ran in the
	// round-5 probe): refuse rather than render inert, the file names the
	// command that supplied the bytes.
	if textsafe.ContainsUnsafe(method) || textsafe.ContainsUnsafe(path) {
		return fmt.Errorf("handler method %q or path %q carries control, invisible, or bidi bytes: they ride a // comment slot and cannot be scaffolded safely", method, path)
	}
	// A keyword does not parse at `func %s(`; main and init redeclare the
	// package's entry points; a predeclared name (string, len, error)
	// shadows it for every other file of the package; _ cannot be called.
	if reason := unusableFuncName(rawName); reason != "" {
		return fmt.Errorf("invalid handler name %q: %s; pick another name", rawName, reason)
	}
	name := rawName
	// Resolve the base dir before joining: a symlinked component would
	// otherwise redirect the scaffolded file outside it undetected
	// (lexical containment cannot see symlinks).
	realDir, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base dir: %w", err)
	}
	// The file itself goes through an os.Root on that directory
	// (projectfs): a <name>_handler.go linked out of it, dangling or not,
	// is refused, never written through. Lstat, not Stat, so a dangling
	// link counts as a file that is already there.
	leaf := strings.ToLower(name) + "_handler.go"
	filename := filepath.Join(realDir, leaf)
	root, err := os.OpenRoot(realDir)
	if err != nil {
		return fmt.Errorf("open base dir: %w", err)
	}
	defer root.Close()
	if !overwrite {
		if _, err := projectfs.Lstat(root, leaf); err == nil {
			return fmt.Errorf("handler file already exists: %s", filename)
		}
	}

	// The file joins the package already in the directory (package main
	// beside main.go, package entities under entities/); writing package
	// main everywhere broke the build with "found packages entities and
	// main". A name that package already declares would be redeclared.
	pkg, declared := dirPackage(realDir, filepath.Base(filename))
	if declared[name] {
		return fmt.Errorf("package %s in %s already declares %s: pick another handler name", pkg, realDir, name)
	}

	content := fmt.Sprintf(`// %s handler: scaffolded by gofastr new handler.
package %s

import (
	"net/http"
)

// %s handles %s %s.
func %s(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{\"ok\": true}"))
}
`, name, pkg, name, method, path, name)

	if err := projectfs.Replace(root, leaf, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}

// unusableFuncName says why name, a Go identifier, cannot be the
// scaffolded handler's function name, or "" when it can.
func unusableFuncName(name string) string {
	switch {
	case token.IsKeyword(name):
		return "it is a Go keyword"
	case name == "main" || name == "init":
		return "func " + name + " is reserved for the package's entry points"
	case name == "_":
		return "a blank function cannot be called"
	case types.Universe.Lookup(name) != nil:
		return "it would shadow Go's predeclared " + name + " in the whole package"
	}
	return ""
}

// dirPackage returns the package the non-test Go files in dir declare
// ("main" when there are none) and the names that package declares at
// top level. skip, the file being scaffolded, is left out: -overwrite
// rewrites it. A file that does not parse is skipped, as go build would
// report it on its own.
func dirPackage(dir, skip string) (string, map[string]bool) {
	pkg := ""
	declared := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == skip || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		if pkg == "" {
			pkg = f.Name.Name
		}
		if f.Name.Name != pkg {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						declared[spec.Name.Name] = true
					case *ast.ValueSpec:
						for _, id := range spec.Names {
							declared[id.Name] = true
						}
					}
				}
			}
		}
	}
	if pkg == "" {
		pkg = "main"
	}
	return pkg, declared
}
