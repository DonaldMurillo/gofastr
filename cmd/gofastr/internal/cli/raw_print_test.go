package cli

//gofastr:test-reads cmd/gofastr/**/*.go

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// scrubbingFormatters lists the calls (package.Func) whose result a raw
// fmt print may carry: a report formatter that colours its own output
// and scrubs every repo-derived piece of it before it does, which a
// scrubbing print would strip of that colour. Every entry says where the
// scrub happens. Anything else that reads the project (a path under it, a
// file's contents, a key name, a spec value, a subprocess's output, an
// error that names any of those) goes through a status helper
// (Info/Warn/Fail/Success) or Printf/Print/Println/Fprintf/Fprintln,
// which scrub every argument, or through ScrubTerminalOutput.
var scrubbingFormatters = map[string]string{
	"contracts.FormatText":     "framework/contracts sanitizeText scrubs every location, message, snippet and suggestion before it adds colour",
	"contracts.FormatExplain":  "the rule comes from the contracts catalog compiled into the binary; the colour is the formatter's",
	"docscmd.highlight":        "it scrubs the excerpt (ScrubTerminalOutput) before it bolds the matches",
	"testcmd.colorizeTestLine": "it scrubs the go test output line (ScrubTerminalOutput) before it colours it",
}

// TestNoRawRepoTextReachesTheTerminal walks the CLI's source for a fmt
// print to the terminal (fmt.Print/Printf/Println, or fmt.Fprint* to
// os.Stdout or os.Stderr) with an argument that is neither a constant
// nor scrubbed. Owner decision (2026-11): repo-derived text never
// reaches the terminal live. The status helpers scrub every argument; a
// raw fmt print with a variable in it bypassed that, as the harness's
// repo-secrets consent prompt did with a cloned repo's key names
// (D-34): an OSC 52 / CSI key name erased the warning and wrote the
// clipboard before the operator answered.
func TestNoRawRepoTextReachesTheTerminal(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(self), "..", "..") // cmd/gofastr
	type file struct {
		rel string
		src []byte
		ast *ast.File
	}
	fset := token.NewFileSet()
	var files []file
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			return nil // a template fragment, not this repo's code
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, file{filepath.ToSlash(rel), src, f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("setup broken: walked only %d Go files under %s", len(files), root)
	}

	// The package-level constants of every package, by directory (for a
	// bare name) and by package name (for pkg.Name from another one).
	constsByDir := map[string]map[string]bool{}
	constsByPkg := map[string]map[string]bool{}
	for _, f := range files {
		dir := filepath.Dir(f.rel)
		for _, decl := range f.ast.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				for _, n := range spec.(*ast.ValueSpec).Names {
					addName(constsByDir, dir, n.Name)
					addName(constsByPkg, f.ast.Name.Name, n.Name)
				}
			}
		}
	}

	used := map[string]bool{}
	var found []string
	for _, f := range files {
		ins := inertness{local: constsByDir[filepath.Dir(f.rel)], pkgs: constsByPkg, pkg: f.ast.Name.Name, inCLI: f.ast.Name.Name == "cli", used: used}
		for _, decl := range f.ast.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				args, ok := terminalPrintArgs(call)
				if !ok {
					return true
				}
				for _, a := range args {
					if !ins.inert(a) {
						found = append(found, fmt.Sprintf("%s (%s): %s", fset.Position(call.Pos()), funcName(fn), exprString(fset, f.src, call)))
						break
					}
				}
				return true
			})
		}
	}
	sort.Strings(found)
	for _, s := range found {
		t.Errorf("SECURITY: [cli-terminal-escape] a raw fmt print to the terminal carries a value that is not scrubbed: %s\n"+
			"\troute it through cli.Printf/Print/Println/Fprintf/Fprintln or a status helper, which scrub every argument", s)
	}
	for _, k := range sortedKeys(scrubbingFormatters) {
		if !used[k] {
			t.Errorf("scrubbingFormatters[%q] names no raw print that needs it; drop the entry", k)
		}
	}
}

func addName(m map[string]map[string]bool, k, name string) {
	if m[k] == nil {
		m[k] = map[string]bool{}
	}
	m[k][name] = true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// terminalPrintArgs returns the arguments of a fmt print that goes
// straight to the terminal. Printf's format counts as one, so a format
// built from a variable is flagged too.
func terminalPrintArgs(call *ast.CallExpr) ([]ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Print", "Printf", "Println":
		return call.Args, true
	case "Fprint", "Fprintf", "Fprintln":
		if len(call.Args) == 0 || !isStdStream(call.Args[0]) {
			return nil, false
		}
		return call.Args[1:], true
	}
	return nil, false
}

func isStdStream(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// inertness decides whether an argument cannot carry text from outside
// the binary: a literal, a constant, a scrubbed value, or the CLI's own
// styling of one of those.
type inertness struct {
	local map[string]bool            // this package's constants
	pkgs  map[string]map[string]bool // every package's, by package name
	pkg   string                     // the package the call is in
	inCLI bool
	used  map[string]bool // the scrubbingFormatters entries met
}

var styleFuncs = map[string]bool{"Red": true, "Green": true, "Yellow": true, "Bold": true, "Dim": true}

func (in inertness) inert(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return in.inert(v.X)
	case *ast.BinaryExpr:
		return in.inert(v.X) && in.inert(v.Y)
	case *ast.Ident:
		switch v.Name {
		case "true", "false", "nil":
			return true
		}
		return (v.Obj != nil && v.Obj.Kind == ast.Con) || (v.Obj == nil && in.local[v.Name])
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		return ok && x.Obj == nil && in.pkgs[x.Name][v.Sel.Name]
	case *ast.CallExpr:
		switch f := v.Fun.(type) {
		case *ast.Ident:
			switch {
			case scrubbingFormatters[in.pkg+"."+f.Name] != "":
				in.used[in.pkg+"."+f.Name] = true
				return true
			case f.Name == "len" || f.Name == "cap":
				return true
			case in.inCLI && (f.Name == "ScrubTerminalOutput" || f.Name == "scrubbedf"):
				return true
			case in.inCLI && styleFuncs[f.Name]:
				return in.all(v.Args)
			}
		case *ast.SelectorExpr:
			x, ok := f.X.(*ast.Ident)
			if !ok {
				return false
			}
			switch {
			case x.Name == "cli" && f.Sel.Name == "ScrubTerminalOutput":
				return true
			case x.Name == "cli" && styleFuncs[f.Sel.Name]:
				return in.all(v.Args)
			case scrubbingFormatters[x.Name+"."+f.Sel.Name] != "":
				in.used[x.Name+"."+f.Sel.Name] = true
				return true
			case x.Name == "strconv" && f.Sel.Name == "Itoa":
				return true // digits and a sign, whatever the int
			case x.Name == "strings" && f.Sel.Name == "Repeat":
				return in.all(v.Args)
			}
		}
	}
	return false
}

func (in inertness) all(args []ast.Expr) bool {
	for _, a := range args {
		if !in.inert(a) {
			return false
		}
	}
	return true
}

// exprString is the source text of n, on one line.
func exprString(fset *token.FileSet, src []byte, n ast.Node) string {
	start, end := fset.Position(n.Pos()).Offset, fset.Position(n.End()).Offset
	if start < 0 || end > len(src) || start > end {
		return "?"
	}
	return strings.Join(strings.Fields(string(src[start:end])), " ")
}
