// Package guardmut finds the conditional guards in a Go source file and
// rewrites each one so it never fires, or always fires:
//
//	if cond {          →  if (cond) && false {     // guard never fires
//	if cond {          →  if (cond) || true {      // guard always fires
package guardmut

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// Kind is the direction a guard is broken in.
type Kind string

const (
	// Never makes the condition false: the guard stops refusing.
	Never Kind = "never"
	// Always makes the condition true: the guard refuses everything.
	Always Kind = "always"
)

// Guard is one mutable condition in a source file.
type Guard struct {
	File  string
	Line  int
	Kind  Kind
	Cond  string // the condition's source text, for the report
	start int    // byte offset of the condition
	end   int
}

// String identifies a guard in output.
func (g Guard) String() string {
	return fmt.Sprintf("%s:%d %s(%s)", g.File, g.Line, g.Kind, g.Cond)
}

// Options tunes which guards are collected.
type Options struct {
	// SkipErrNil drops conditions that are exactly `err != nil` or `err == nil`.
	SkipErrNil bool
}

// Find returns every mutable guard in src, both directions, in source order.
func Find(file string, src []byte, opts Options) ([]Guard, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	var out []Guard
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Cond == nil {
			return true
		}
		start := fset.Position(ifs.Cond.Pos()).Offset
		end := fset.Position(ifs.Cond.End()).Offset
		cond := string(src[start:end])
		if opts.SkipErrNil && isErrNil(ifs.Cond) {
			return true
		}
		line := fset.Position(ifs.Cond.Pos()).Line
		for _, k := range []Kind{Never, Always} {
			out = append(out, Guard{File: file, Line: line, Kind: k, Cond: cond, start: start, end: end})
		}
		return true
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func isErrNil(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return false
	}
	ident, ok := bin.X.(*ast.Ident)
	if !ok || ident.Name != "err" {
		return false
	}
	nilIdent, ok := bin.Y.(*ast.Ident)
	return ok && nilIdent.Name == "nil"
}

// Apply returns src with g's condition rewritten, or an error when g's
// offsets do not fit src or its kind is unknown.
func Apply(src []byte, g Guard) ([]byte, error) {
	if g.end > len(src) || g.start >= g.end {
		return nil, fmt.Errorf("guard offsets out of range for %s", g)
	}
	var suffix string
	switch g.Kind {
	case Never:
		suffix = ") && false"
	case Always:
		suffix = ") || true"
	default:
		return nil, fmt.Errorf("unknown mutation kind %q", g.Kind)
	}
	out := make([]byte, 0, len(src)+len(suffix)+1)
	out = append(out, src[:g.start]...)
	out = append(out, '(')
	out = append(out, src[g.start:g.end]...)
	out = append(out, suffix...)
	out = append(out, src[g.end:]...)
	return out, nil
}
