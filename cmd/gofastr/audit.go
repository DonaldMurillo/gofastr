package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AuditFinding is one call site of a tracked init-time registration
// API. The Kind is the "pkg.Func" name (e.g. "style.Contribute") of the
// API being called.
type AuditFinding struct {
	Pkg  string // Go import path of the calling package
	Kind string // "style.Contribute", "render.RegisterComponent", …
	File string // relative path from the audited root
	Line int
}

// trackedKinds is the set of (importPath, packageAlias, functionName)
// tuples that the audit reports. Each entry says: when a file imports
// `importPath`, calls to `alias.FunctionName(...)` are init-time
// global-registration side effects worth flagging in an audit.
//
// Extend this list as the framework grows new init-time hooks.
var trackedKinds = []struct {
	ImportPath  string
	PackageName string // the import alias most callers use
	FuncName    string
	Kind        string // label shown in the report
}{
	{"github.com/DonaldMurillo/gofastr/core-ui/style", "style", "Contribute", "style.Contribute"},
	{"github.com/DonaldMurillo/gofastr/core-ui/registry", "registry", "RegisterStyle", "registry.RegisterStyle"},
	{"github.com/DonaldMurillo/gofastr/core/render", "render", "RegisterComponent", "render.RegisterComponent"},
	{"github.com/DonaldMurillo/gofastr/core/render", "render", "RegisterLayout", "render.RegisterLayout"},
	{"github.com/DonaldMurillo/gofastr/core/render", "render", "RegisterFunc", "render.RegisterFunc"},
}

// auditDeps walks every .go file under root (skipping vendor, .git, and
// hidden dirs) and returns one AuditFinding per call to a tracked
// init-time registration API.
//
// The root must contain a go.mod so package import paths can be
// computed; modules without go.mod fall back to relative paths.
func auditDeps(root string) ([]AuditFinding, error) {
	modulePath := readModulePath(root)
	var findings []AuditFinding

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == ".git" || strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fs, perr := parseFile(path)
		if perr != nil {
			// Parse errors aren't fatal — we want the audit to keep
			// going even when the project is mid-edit.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		pkgPath := importPathFor(modulePath, root, filepath.Dir(path))
		findings = append(findings, scanFile(fs, pkgPath, rel)...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Pkg != findings[j].Pkg {
			return findings[i].Pkg < findings[j].Pkg
		}
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

type parsedFile struct {
	fset *token.FileSet
	f    *ast.File
}

func parseFile(path string) (*parsedFile, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &parsedFile{fset: fset, f: f}, nil
}

// scanFile resolves the file's import aliases against trackedKinds and
// returns one AuditFinding per qualified-call (alias.FuncName) it
// matches.
func scanFile(pf *parsedFile, pkg, rel string) []AuditFinding {
	// Resolve aliases: which local alias maps to which import path.
	aliasOf := make(map[string]string)
	for _, imp := range pf.f.Imports {
		ipath := strings.Trim(imp.Path.Value, `"`)
		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			alias = filepath.Base(ipath)
		}
		aliasOf[alias] = ipath
	}

	// Build the set of (alias, funcName) → kind we care about for THIS
	// file's import map.
	want := make(map[[2]string]string)
	for _, tk := range trackedKinds {
		for alias, ipath := range aliasOf {
			if ipath == tk.ImportPath && alias == tk.PackageName {
				want[[2]string{alias, tk.FuncName}] = tk.Kind
			}
		}
	}
	if len(want) == 0 {
		return nil
	}

	var out []AuditFinding
	ast.Inspect(pf.f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		kind, ok := want[[2]string{ident.Name, sel.Sel.Name}]
		if !ok {
			return true
		}
		pos := pf.fset.Position(call.Pos())
		out = append(out, AuditFinding{
			Pkg:  pkg,
			Kind: kind,
			File: rel,
			Line: pos.Line,
		})
		return true
	})
	return out
}

// formatAuditReport renders findings into a human-readable text block,
// grouped by package, with per-(pkg,kind) counts and per-call-site
// pointers. Quiet when findings is empty.
func formatAuditReport(findings []AuditFinding) string {
	if len(findings) == 0 {
		return "No init-time global registrations found.\n"
	}

	var b strings.Builder
	b.WriteString("Init-time global registrations\n")
	b.WriteString("(packages whose init() can mutate framework-wide state)\n\n")

	// Group: pkg → kind → []findings
	type group map[string][]AuditFinding
	byPkg := make(map[string]group)
	for _, f := range findings {
		if byPkg[f.Pkg] == nil {
			byPkg[f.Pkg] = make(group)
		}
		byPkg[f.Pkg][f.Kind] = append(byPkg[f.Pkg][f.Kind], f)
	}

	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, p := range pkgs {
		fmt.Fprintf(&b, "%s\n", p)
		kinds := byPkg[p]
		kindNames := make([]string, 0, len(kinds))
		for k := range kinds {
			kindNames = append(kindNames, k)
		}
		sort.Strings(kindNames)
		for _, k := range kindNames {
			entries := kinds[k]
			fmt.Fprintf(&b, "  %s ×%d\n", k, len(entries))
			for _, e := range entries {
				fmt.Fprintf(&b, "    %s:%d\n", e.File, e.Line)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// runAudit is the CLI entry point. Subcommand: `gofastr audit deps`.
func runAudit(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: gofastr audit <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  deps    List packages that perform init-time global registrations")
		fmt.Println("  lint    Scan for AI-typical mistakes (ignored Exec, missing CSRF, render.HTML concat, t.Skip, …)")
		osExit(2)
	}
	switch args[0] {
	case "deps":
		root := "."
		if len(args) > 1 {
			root = args[1]
		}
		findings, err := auditDeps(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "audit deps: %v\n", err)
			osExit(1)
		}
		fmt.Print(formatAuditReport(findings))
	case "lint":
		root := "."
		if len(args) > 1 {
			root = args[1]
		}
		findings, err := auditLint(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "audit lint: %v\n", err)
			osExit(1)
		}
		fmt.Print(formatLintReport(findings))
		if len(findings) > 0 {
			osExit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown audit subcommand: %s\n", args[0])
		osExit(2)
	}
}

// readModulePath returns the `module` line from a go.mod at root, or
// empty when none is found. Used to derive package import paths from
// filesystem locations.
func readModulePath(root string) string {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}

// importPathFor maps a directory back to its Go import path given the
// module's root + module-path declaration.
func importPathFor(modulePath, root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return modulePath
	}
	if modulePath == "" {
		return rel
	}
	return modulePath + "/" + rel
}

// mkdirAll + writeFile are small wrappers used by the audit's tests
// (and trivially elsewhere) so the test file doesn't have to import
// "os" directly.
func mkdirAll(path string) error         { return os.MkdirAll(path, 0o755) }
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
