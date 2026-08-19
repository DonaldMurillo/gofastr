// Package guardmut finds the conditional guards in a Go source file and
// rewrites them so they can never fire, or always fire.
//
// It exists because of a failure this repository kept repeating: a test that
// names a guard, passes, and proves nothing — because the fixture it uses
// satisfies TWO refusal conditions at once, so removing either one alone
// leaves the test green. Seven such tests were found by hand across one
// review cycle. No static check can see it: whether a fixture reaches a
// particular branch is a runtime property. Breaking the guard and re-running
// the tests is the only detector, so this package makes that mechanical.
//
// The rewrite is deliberately an ANNOTATION of the original expression rather
// than a replacement of it:
//
//	if cond {          →  if (cond) && false {     // guard never fires
//	if cond {          →  if (cond) || true {      // guard always fires
//
// Every identifier in the condition is still referenced, so no local goes
// unused and no import is orphaned — the mutated file compiles whenever the
// original did. Hand-written mutations failed this way twice during the review
// that motivated this tool, and a mutation that fails to compile (or fails to
// apply) is indistinguishable from one the tests did not catch: both end the
// run without a test failure.
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
	// Never makes the condition false: the guard stops refusing. A surviving
	// Never mutant means no test proves the guard refuses anything.
	Never Kind = "never"
	// Always makes the condition true: the guard refuses everything. A
	// surviving Always mutant means no test proves the guard PERMITS
	// anything — the missing allow-arm that lets a too-tight gate ship.
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
	// SkipErrNil drops `if err != nil` conditions. Error plumbing is guarded
	// almost everywhere and tested almost nowhere, so including it buries the
	// interesting survivors under hundreds of expected ones. Off by default
	// in the CLI; set false to audit error paths deliberately.
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
		// The offsets come from parsing THIS src, so they are always inside it
		// and always non-empty — a valid IfStmt cannot have an empty
		// condition. A bounds check here would be unreachable, which this tool
		// pointed out when run on itself. Apply() does check, because it can be
		// handed a Guard measured against different source.
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

// isErrNil reports whether cond is exactly `err != nil` or `err == nil`.
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

// Apply returns src with g's condition rewritten. The result is guaranteed to
// differ from src — callers should verify that, because a mutation that did
// not land looks exactly like one the tests failed to catch.
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
