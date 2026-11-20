package newcmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
	"github.com/DonaldMurillo/gofastr/core/router"
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
	// Check before the fold: strings.ToUpper maps ı to I and ſ to S,
	// so a non-ASCII method would pass as a different token.
	if err := checkRouteMethod(method); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	method = strings.ToUpper(method)

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
	// Check before the fold: strings.ToUpper maps ı to I and ſ to S,
	// so a non-ASCII method would pass as a different token.
	if err := checkRouteMethod(method); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	method = strings.ToUpper(method)
	// The handler lands at an expression slot of the printed Go line: a
	// name that is not an identifier, or a selector of them (h.List),
	// prints a line that does not compile.
	if err := checkRouteHandler(handler); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsFunc(path, unicode.IsSpace) {
		cli.Fail("invalid route pattern %q: path must start with / and must not contain whitespace", path)
		cli.OsExit(1)
		return
	}
	if err := router.ValidatePattern(method, path); err != nil {
		cli.Fail("invalid route pattern %q: %v", path, err)
		cli.OsExit(1)
		return
	}

	cli.Info("Add this to your app setup:")
	cli.Printf("\n  %s\n\n", routeSnippet(method, path, handler))
	// Router.Handle takes an http.Handler. new handler scaffolds a plain
	// func, which needs the conversion; a handler value does not, and the
	// command cannot tell which the name is.
	cli.Info("If %s is a func(http.ResponseWriter, *http.Request), such as one new handler wrote, pass http.HandlerFunc(%s).", handler, handler)
}

// checkRouteMethod refuses a method that is not an HTTP token (RFC 9110
// section 5.6.2): the router builds a "METHOD /path" pattern from it, and
// net/http's ServeMux panics on a pattern whose method is no token.
// Extension methods such as PROPFIND are tokens and pass.
func checkRouteMethod(method string) error {
	if method == "" {
		return fmt.Errorf("invalid method %q: must be an HTTP method such as GET or PROPFIND", method)
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0 {
			continue
		}
		return fmt.Errorf("invalid method %q: must be an HTTP method such as GET or PROPFIND", method)
	}
	return nil
}

// checkRouteHandler refuses a --handler that is not a Go identifier or a
// selector chain of them (h.List, handlers.Things). The handler is the
// host's own Go name, so any identifier the Go spec allows passes, Unicode
// letters included. A bare name gets new handler's guard too: a keyword,
// main, init, _ or a predeclared name is no handler.
func checkRouteHandler(handler string) error {
	parts := strings.Split(handler, ".")
	for _, p := range parts {
		if !token.IsIdentifier(p) || token.IsKeyword(p) || p == "_" {
			return fmt.Errorf("invalid handler %q: must be a Go identifier or a selector such as h.List", handler)
		}
	}
	if len(parts) == 1 {
		if reason := unusableFuncName(handler); reason != "" {
			return fmt.Errorf("invalid handler %q: %s; pick another name", handler, reason)
		}
		_, _, imports := dirPackage(".", "")
		if conflict := imports[handler]; conflict != "" {
			return fmt.Errorf("invalid handler %q: %s; pick another name", handler, conflict)
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
	const handlerTemplate = `// %s handler: scaffolded by gofastr new handler.
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
`

	f, err := parser.ParseFile(token.NewFileSet(), filename, fmt.Sprintf(handlerTemplate, name, "main", name, method, path, name), parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse generated handler: %w", err)
	}
	pkg, declared, imports := dirPackage(realDir, filepath.Base(filename), f)
	if declared[name] {
		return fmt.Errorf("package %s in %s already declares %s: pick another handler name", pkg, realDir, name)
	}
	if conflict := imports[name]; conflict != "" {
		return fmt.Errorf("invalid handler name %q: %s; pick another name", name, conflict)
	}
	content := fmt.Sprintf(handlerTemplate, name, pkg, name, method, path, name)

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
func dirPackage(dir, skip string, generated ...*ast.File) (string, map[string]bool, map[string]string) {
	pkg := ""
	declared := map[string]bool{}
	imports := map[string]string{}
	type sourceFile struct {
		name string
		file *ast.File
	}
	var files []sourceFile
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == skip || !strings.HasSuffix(n, ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		if pkg == "" && !strings.HasSuffix(n, "_test.go") {
			pkg = f.Name.Name
		}
		files = append(files, sourceFile{n, f})
	}
	if pkg == "" {
		pkg = "main"
	}
	var importFiles []sourceFile
	for _, source := range files {
		f := source.file
		if f.Name.Name != pkg {
			continue
		}
		importFiles = append(importFiles, source)
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
	for _, f := range generated {
		importFiles = append(importFiles, sourceFile{"the generated handler", f})
	}
	var astFiles []*ast.File
	for _, source := range importFiles {
		astFiles = append(astFiles, source.file)
	}
	resolved := resolveImportNames(dir, astFiles)
	for _, source := range importFiles {
		for name, conflict := range importNames(source.file, source.name, resolved) {
			if imports[name] == "" {
				imports[name] = conflict
			}
		}
	}
	return pkg, declared, imports
}

func importNames(f *ast.File, source string, resolved map[string]string) map[string]string {
	names := map[string]string{}
	for _, spec := range f.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := assumedImportName(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		} else if resolved[importPath] != "" {
			name = resolved[importPath]
		}
		if name != "_" && name != "." {
			names[name] = fmt.Sprintf("conflicts with the import of %q in %s", importPath, source)
		}
	}
	return names
}

func resolveImportNames(dir string, files []*ast.File) map[string]string {
	var paths []string
	seen := map[string]bool{}
	for _, f := range files {
		for _, spec := range f.Imports {
			if spec.Name != nil {
				continue
			}
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err == nil && !seen[importPath] {
				seen[importPath] = true
				paths = append(paths, importPath)
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"list", "-e", "-find", "-f", "{{.ImportPath}}\t{{.Name}}", "--"}, paths...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	names := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		importPath, name, ok := strings.Cut(line, "\t")
		if ok && seen[importPath] && token.IsIdentifier(name) {
			names[importPath] = name
		}
	}
	return names
}

func assumedImportName(importPath string) string {
	name := path.Base(importPath)
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" && path.Dir(importPath) != "." {
		name = path.Base(path.Dir(importPath))
	}
	name = strings.TrimPrefix(name, "go-")
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return name[:i]
		}
	}
	return name
}
