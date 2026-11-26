// Package combinedparse catches a command's merged stdout and stderr
// read as a value: parsed, compared, or handed back to the caller.
//
// The bug (round 12 S3-02, 94e342b64): the deploy runner ran ssh with
// `cmd.Stdout, cmd.Stderr = &out, &out` and returned the one buffer.
// deploy split it with strings.Fields and expected two lines, `uname -m`
// and the mktemp directory. On first contact ssh prints "Warning:
// Permanently added ... to the list of known hosts." on stderr, so the
// split saw extra words and the first deploy to every new server
// failed. The install step's sentinel parse broke the same way. The fix
// reads stdout as the value and keeps stderr for the error message.
//
// Shape: inside one function, a merged output (the []byte from
// (*exec.Cmd).CombinedOutput, or a buffer whose address is assigned to
// both Stdout and Stderr of one exec.Cmd, read through its String or
// Bytes method) reaches a value use:
//   - an argument to a parser: strings or bytes Fields, Split, Lines,
//     Cut, HasPrefix and their kin, strconv.Parse* and Atoi,
//     json.Unmarshal, a json.Decoder or bufio.Scanner over it, or a
//     regexp Find or Match method;
//   - a comparison with a non-empty constant, or a switch tag;
//   - a non-error result of a return statement, or an assignment to a
//     struct field.
//
// The merged output stays tainted through string and []byte
// conversions, the strings and bytes trim and case helpers, a reader
// over it (strings.NewReader, bytes.NewReader, bytes.NewBuffer), and a
// local assigned from any of those.
//
// Silent postures, deliberately:
//   - the output only reaches an error, a log line or a writer
//     (fmt.Errorf, t.Fatalf, Fprintf, a logger): stderr is what the
//     reader of an error message wants;
//   - strings.Contains and Index on the output: matching a known
//     message in either stream is how callers detect a tool's
//     complaint, and the merged stream is the right input for it;
//   - a len check, an empty-string comparison, or a call to a
//     package-local function: the taint does not follow calls;
//   - _test.go files.
package combinedparse

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "combinedparse",
	Doc:  "report merged stdout+stderr (CombinedOutput, or one buffer on both streams) parsed, compared or returned as a value: a warning on stderr breaks the parse",
	Run:  run,
}

// parsers are the functions that read their argument as data.
var parsers = map[string]bool{}

func init() {
	for _, pkg := range []string{"strings", "bytes"} {
		for _, fn := range []string{"Fields", "FieldsFunc", "FieldsSeq", "FieldsFuncSeq", "Split", "SplitN", "SplitAfter", "SplitAfterN", "SplitSeq", "SplitAfterSeq", "Lines", "Cut", "CutPrefix", "CutSuffix", "HasPrefix", "HasSuffix", "EqualFold"} {
			parsers[pkg+"."+fn] = true
		}
	}
	for _, fn := range []string{"Atoi", "ParseInt", "ParseUint", "ParseFloat", "ParseBool", "Unquote"} {
		parsers["strconv."+fn] = true
	}
	parsers["encoding/json.Unmarshal"] = true
	parsers["encoding/json.NewDecoder"] = true
	parsers["bufio.NewScanner"] = true
	parsers["bufio.NewReader"] = true
	for _, fn := range []string{"Find", "FindAll", "FindAllIndex", "FindAllString", "FindAllStringIndex", "FindAllStringSubmatch", "FindAllSubmatch", "FindIndex", "FindString", "FindStringIndex", "FindStringSubmatch", "FindSubmatch", "Match", "MatchString", "ReplaceAll", "ReplaceAllString", "Split"} {
		parsers["(*regexp.Regexp)."+fn] = true
	}
}

// passthrough functions keep their first argument's taint.
var passthrough = map[string]bool{
	"strings.TrimSpace": true, "strings.Trim": true, "strings.TrimLeft": true, "strings.TrimRight": true,
	"strings.TrimPrefix": true, "strings.TrimSuffix": true, "strings.ToLower": true, "strings.ToUpper": true,
	"bytes.TrimSpace": true, "bytes.Trim": true, "bytes.TrimLeft": true, "bytes.TrimRight": true,
	"bytes.TrimPrefix": true, "bytes.TrimSuffix": true, "bytes.ToLower": true, "bytes.ToUpper": true,
	"strings.NewReader": true, "bytes.NewReader": true, "bytes.NewBuffer": true, "bytes.NewBufferString": true,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					checkFunc(pass, d.Type, d.Body)
				}
			case *ast.GenDecl:
				// var runTool = func(...) (string, error) { ... }: a
				// test seam is a function all the same.
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, v := range vs.Values {
							if lit, ok := ast.Unparen(v).(*ast.FuncLit); ok {
								checkFunc(pass, lit.Type, lit.Body)
							}
						}
					}
				}
			}
		}
	}
	return nil, nil
}

type checker struct {
	pass *analysis.Pass
	// tainted holds the locals that carry merged output.
	tainted map[types.Object]bool
	// merged holds the buffers wired to both streams of one Cmd.
	merged map[types.Object]bool
	// reported dedupes one finding per line.
	reported map[int]bool
}

func checkFunc(pass *analysis.Pass, ft *ast.FuncType, body *ast.BlockStmt) {
	c := &checker{pass: pass, tainted: map[types.Object]bool{}, merged: map[types.Object]bool{}, reported: map[int]bool{}}
	c.findMergedBuffers(body)
	// Two passes reach a local assigned before the one it reads in
	// source order (a loop body, a closure).
	for range 2 {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				c.propagate(x.Lhs, x.Rhs)
			case *ast.ValueSpec:
				lhs := make([]ast.Expr, len(x.Names))
				for i, id := range x.Names {
					lhs[i] = id
				}
				c.propagate(lhs, x.Values)
			}
			return true
		})
	}
	if len(c.tainted) == 0 && len(c.merged) == 0 {
		return
	}
	c.findSinks(ft, body)
}

// findMergedBuffers records a buffer whose address feeds both Stdout and
// Stderr of the same *exec.Cmd.
func (c *checker) findMergedBuffers(body *ast.BlockStmt) {
	stdout := map[types.Object]types.Object{}
	stderr := map[types.Object]types.Object{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Stdout" && sel.Sel.Name != "Stderr") || !c.isCmd(sel.X) {
				continue
			}
			cmd := c.objOf(sel.X)
			buf := c.bufferOf(as.Rhs[i])
			if cmd == nil || buf == nil {
				continue
			}
			if sel.Sel.Name == "Stdout" {
				stdout[cmd] = buf
			} else {
				stderr[cmd] = buf
			}
		}
		return true
	})
	for cmd, buf := range stdout {
		if stderr[cmd] == buf {
			c.merged[buf] = true
		}
	}
}

func (c *checker) isCmd(x ast.Expr) bool {
	t := c.pass.TypesInfo.TypeOf(x)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "os/exec" && n.Obj().Name() == "Cmd"
}

// objOf resolves an identifier, or the root identifier of a selector
// chain, to its object.
func (c *checker) objOf(x ast.Expr) types.Object {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		return c.pass.TypesInfo.ObjectOf(e)
	case *ast.SelectorExpr:
		return c.pass.TypesInfo.ObjectOf(e.Sel)
	}
	return nil
}

// bufferOf resolves &buf or buf to buf's object.
func (c *checker) bufferOf(x ast.Expr) types.Object {
	x = ast.Unparen(x)
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		x = ast.Unparen(u.X)
	}
	if id, ok := x.(*ast.Ident); ok {
		return c.pass.TypesInfo.ObjectOf(id)
	}
	return nil
}

func (c *checker) propagate(lhs, rhs []ast.Expr) {
	if len(rhs) == 1 && len(lhs) >= 1 {
		if c.isCombinedCall(rhs[0]) || (len(lhs) == 1 && c.taintedExpr(rhs[0])) {
			c.taint(lhs[0])
		}
		return
	}
	if len(lhs) != len(rhs) {
		return
	}
	for i := range lhs {
		if c.taintedExpr(rhs[i]) {
			c.taint(lhs[i])
		}
	}
}

func (c *checker) taint(x ast.Expr) {
	if id, ok := x.(*ast.Ident); ok && id.Name != "_" {
		if obj := c.pass.TypesInfo.ObjectOf(id); obj != nil {
			c.tainted[obj] = true
		}
	}
}

func (c *checker) isCombinedCall(x ast.Expr) bool {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := c.callee(call)
	return fn != nil && fn.FullName() == "(*os/exec.Cmd).CombinedOutput"
}

func (c *checker) callee(call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, _ := c.pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// taintedExpr reports whether x carries merged output.
func (c *checker) taintedExpr(x ast.Expr) bool {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		return c.tainted[c.pass.TypesInfo.ObjectOf(e)]
	case *ast.SliceExpr:
		return c.taintedExpr(e.X)
	case *ast.BinaryExpr:
		return e.Op == token.ADD && (c.taintedExpr(e.X) || c.taintedExpr(e.Y))
	case *ast.CallExpr:
		if c.isCombinedCall(e) {
			return true
		}
		// A conversion: string(out), []byte(s).
		if tv, ok := c.pass.TypesInfo.Types[e.Fun]; ok && tv.IsType() && len(e.Args) == 1 {
			return c.taintedExpr(e.Args[0])
		}
		fn := c.callee(e)
		if fn == nil {
			return false
		}
		// buf.String() / buf.Bytes() on a merged buffer.
		if sel, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr); ok && (fn.Name() == "String" || fn.Name() == "Bytes") {
			if obj := c.bufferOf(sel.X); obj != nil && c.merged[obj] {
				return true
			}
		}
		if passthrough[fn.FullName()] && len(e.Args) > 0 {
			return c.taintedExpr(e.Args[0])
		}
	}
	return false
}

func (c *checker) findSinks(ft *ast.FuncType, body *ast.BlockStmt) {
	results := c.resultTypes(ft)
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			// A closure's returns are its own; its parser calls and
			// comparisons still count, so walk them without the
			// enclosing results.
			c.findSinks(x.Type, x.Body)
			return false
		case *ast.CallExpr:
			fn := c.callee(x)
			if fn != nil && parsers[fn.FullName()] {
				for _, a := range x.Args {
					if c.taintedExpr(a) {
						c.report(x, "%s reads the merged stdout and stderr: a warning on stderr changes what it sees; read stdout alone (cmd.Output, or separate buffers) and keep stderr for the error", fn.Name())
						break
					}
				}
			}
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && c.comparesToText(x) {
				c.report(x, "merged stdout and stderr compared as a value: a warning on stderr changes the result; compare stdout alone")
			}
		case *ast.SwitchStmt:
			if x.Tag != nil && c.taintedExpr(x.Tag) {
				c.report(x.Tag, "merged stdout and stderr used as a switch value: a warning on stderr changes the result; switch on stdout alone")
			}
		case *ast.ReturnStmt:
			for i, r := range x.Results {
				if i < len(results) && !isError(results[i]) && c.taintedExpr(r) {
					c.report(r, "merged stdout and stderr returned as a value: the caller parses stderr warnings with it; return stdout alone and put stderr in the error")
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i, lhs := range x.Lhs {
					if _, ok := lhs.(*ast.SelectorExpr); ok && c.taintedExpr(x.Rhs[i]) {
						c.report(x.Rhs[i], "merged stdout and stderr stored as a value: a warning on stderr is stored with it; store stdout alone")
					}
				}
			}
		}
		return true
	})
}

func (c *checker) comparesToText(be *ast.BinaryExpr) bool {
	for _, pair := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
		if !c.taintedExpr(pair[0]) {
			continue
		}
		tv, ok := c.pass.TypesInfo.Types[pair[1]]
		if ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) != "" {
			return true
		}
	}
	return false
}

func (c *checker) resultTypes(ft *ast.FuncType) []types.Type {
	var out []types.Type
	if ft.Results == nil {
		return nil
	}
	for _, f := range ft.Results.List {
		t := c.pass.TypesInfo.TypeOf(f.Type)
		n := max(len(f.Names), 1)
		for range n {
			out = append(out, t)
		}
	}
	return out
}

func isError(t types.Type) bool {
	return t != nil && types.Identical(t, types.Universe.Lookup("error").Type())
}

func (c *checker) report(n ast.Node, format string, args ...any) {
	line := c.pass.Fset.Position(n.Pos()).Line
	if c.reported[line] {
		return
	}
	c.reported[line] = true
	c.pass.Reportf(n.Pos(), format, args...)
}
