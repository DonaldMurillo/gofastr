package auditcmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

// AuditFinding is one candidate call site of a tracked global registration API.
type AuditFinding struct {
	Pkg  string // Package label derived from the selected root
	Kind string // "style.Contribute", "render.RegisterComponent", …
	File string // relative path from the audited root
	Line int
}

// trackedKinds lists the global registration APIs the audit matches by import and call syntax.
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

// auditDeps walks non-test .go files under root, skipping vendor,
// node_modules, dist, bin, build, tmp, and hidden/underscore descendants.
func auditDeps(root string) ([]AuditFinding, error) {
	modulePath := contracts.ReadModulePath(root)
	var findings []AuditFinding

	err := sourcewalk.Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parseFile(path, source)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		pkgPath := contracts.ImportPathFor(modulePath, root, filepath.Dir(path))
		findings = append(findings, scanFile(parsed, pkgPath, rel)...)
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

func parseFile(path string, source []byte) (*parsedFile, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &parsedFile{fset: fset, f: f}, nil
}

// scanFile resolves the file's import aliases against trackedKinds and
// returns one AuditFinding per qualified-call (alias.FuncName) it
// matches.
func scanFile(pf *parsedFile, pkg, rel string) []AuditFinding {
	// Resolve each tracked import to the name the file calls it by: its
	// package name, or the name the import gives it. A dot import calls
	// the function bare, keyed under ".". Matching the usual name alone
	// missed cuistyle.Contribute and a dot-imported Contribute.
	want := make(map[[2]string]string)
	for _, imp := range pf.f.Imports {
		ipath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		for _, tk := range trackedKinds {
			if ipath != tk.ImportPath {
				continue
			}
			alias := tk.PackageName
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			if alias == "_" {
				continue
			}
			want[[2]string{alias, tk.FuncName}] = tk.Kind
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
		var key [2]string
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			ident, ok := fn.X.(*ast.Ident)
			if !ok {
				return true
			}
			key = [2]string{ident.Name, fn.Sel.Name}
		case *ast.Ident:
			key = [2]string{".", fn.Name}
		default:
			return true
		}
		kind, ok := want[key]
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
		return "No candidate global registration call sites found.\n"
	}

	var b strings.Builder
	b.WriteString("Candidate global registration call sites\n\n")

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

// RunAudit is the CLI entry point. Subcommand: `gofastr audit deps`.
func RunAudit(args []string) {
	auditUsage := func() {
		fmt.Println("Usage: gofastr audit <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  deps    Find candidate global registration calls in a source tree")
		fmt.Println("  a11y    Accessibility audit: static lint of core-ui/html usage, or a full")
		fmt.Println("          axe-core scan of a running app with --url (see `gofastr audit a11y --help`)")
		fmt.Println()
		fmt.Println("`gofastr verify` covers the source rules and the static half of a11y, with a")
		fmt.Println("reason and a fix attached to every finding, a config file, and per-line")
		fmt.Println("suppression. `audit` remains for what verify does not do: the runtime")
		fmt.Println("browser scan (`a11y --url`) and the registration report (`deps`).")
	}
	// `audit --help` (or a help flag on deps) prints the usage and
	// exits 0. Asking for help is not a usage error. a11y owns its
	// flags and prints its own richer help, and an unknown subcommand
	// exits 2 even with a help flag.
	if len(args) > 0 && (args[0] == "deps" || args[0] == "--help" || args[0] == "-h") {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				auditUsage()
				cli.OsExit(0)
			}
		}
	}
	if len(args) == 0 {
		auditUsage()
		cli.OsExit(2)
	}
	switch args[0] {
	case "deps":
		root := "."
		if len(args) > 1 {
			root = args[1]
		}
		findings, err := auditDeps(root)
		if err != nil {
			cli.Fprintf(os.Stderr, "audit deps: %v\n", err)
			cli.OsExit(1)
		}
		cli.Print(formatAuditReport(findings))
		if _, entries, err := project.UnmountedSettings(root); err != nil {
			cli.Fprintf(os.Stderr, "audit deps: %v\n", err)
			cli.OsExit(1)
		} else if len(entries) > 0 {
			fmt.Println()
			fmt.Println("Unmounted gofastr packages (copies this app owns; framework fixes no longer reach them):")
			for _, e := range entries {
				cli.Printf("  %s\n", project.DescribeUnmounted(e))
			}
		}
	case "a11y":
		runAuditA11y(args[1:])
	default:
		cli.Fprintf(os.Stderr, "Unknown audit subcommand: %s\n", args[0])
		cli.OsExit(2)
	}
}

// mkdirAll + writeFile are small wrappers used by the audit's tests
// (and trivially elsewhere) so the test file doesn't have to import
// "os" directly.
func mkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
