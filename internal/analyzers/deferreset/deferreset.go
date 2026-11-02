// Package deferreset catches state set before a call that can panic and
// reset after it outside a defer: a panic skips the reset, and the state
// stays set for every later caller.
//
// Found in core/middleware, twice in one round:
//   - c0d6e99e: Idempotency claimed the key with cfg.Store.Begin, ran
//     next.ServeHTTP, and released or persisted the claim with finish(…)
//     after it. A panicking handler (http.ErrAbortHandler included)
//     skipped the release, so every retry of the request answered 409 "in
//     flight" until the claim's TTL although nothing was running. The fix
//     releases the claim in a deferred recover and re-panics.
//   - 39de7827: Timeout's writer set tw.hijacking = true, called the
//     wrapped writer's Hijack, and cleared the flag after it. A Hijack
//     that panicked (a wrapper asserting http.Hijacker unchecked) left
//     the flag set, so expire waited on it forever and the request
//     goroutine hung. The fix clears it in a defer.
//
// Shape, within one function body (a declaration or a literal, each on
// its own, nested literals not included):
//   - the SET: a bool struct field assigned true (`tw.hijacking = true`),
//     or a call whose camel-case first word is begin, claim, acquire or
//     reserve (`cfg.Store.Begin(…)`, `q.claim(…)`);
//   - then the CALL that can panic into it: `X.ServeHTTP(…)` (the next
//     handler), a call through a func-typed struct field or parameter
//     (a host callback), or a Hijack;
//   - then the RESET, outside any defer: the same field assigned false,
//     or a call whose first word is release, finish, end, done, abort,
//     complete or unclaim.
//
// The fix spelling: put the reset in a defer, so it runs however the call
// returns (`defer func() { tw.mu.Lock(); tw.hijacking = false; … }()`),
// or, when the reset needs the call's result, a deferred recover that
// resets and re-panics (`defer func() { if v := recover(); v != nil {
// finish(nil); panic(v) } }()`).
//
// Silent postures, deliberately:
//   - the reset (any spelling of it) is deferred somewhere in the body:
//     the deferred closure covers the panic path even when a normal-path
//     reset also follows the call;
//   - nothing that can panic between set and reset: plain package code
//     between them is the function's own business (a panic there is a bug
//     in this code, not a host's);
//   - a set with no reset after the panic point (state that stays set on
//     purpose, like a wroteHeader latch);
//   - a func-typed local as the call (`step := func(){…}; step()`, or a
//     named function value it picked): the function's own code, not a
//     callback — only a func-typed parameter or struct field is one;
//   - _test.go files.
package deferreset

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "deferreset",
	Doc:  "report state set before a call that can panic and reset after it outside a defer",
	Run:  run,
}

var (
	setVerbs   = map[string]bool{"begin": true, "claim": true, "acquire": true, "reserve": true}
	resetVerbs = map[string]bool{"release": true, "finish": true, "end": true, "done": true, "abort": true, "complete": true, "unclaim": true}
)

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			params := map[types.Object]bool{}
			addParams(pass, fd.Type, params)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					addParams(pass, lit.Type, params)
				}
				return true
			})
			check(pass, fd.Body, params)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					check(pass, lit.Body, params)
				}
				return true
			})
		}
	}
	return nil, nil
}

// event is one step of the shape, in source order.
type event struct {
	pos  token.Pos
	kind int    // evSet, evCall, evReset
	key  string // the field for a flag, "" for a verb call
	desc string
}

const (
	evSet = iota
	evCall
	evReset
)

// check walks body's own statements (not nested literals) and reports a
// reset that follows a panic point that follows its set, unless the reset
// is also deferred.
func check(pass *analysis.Pass, body *ast.BlockStmt, params map[types.Object]bool) {
	var events []event
	deferredFlags := map[string]bool{}
	deferredVerb := false
	inDefer := map[ast.Node]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if d, ok := n.(*ast.DeferStmt); ok {
			// Everything a defer runs, its literal's body included, is
			// on the panic path.
			ast.Inspect(d.Call, func(m ast.Node) bool {
				inDefer[m] = true
				switch m := m.(type) {
				case *ast.AssignStmt:
					if key, val, ok := boolFieldAssign(pass, m); ok && !val {
						deferredFlags[key] = true
					}
				case *ast.CallExpr:
					if resetVerbs[firstWord(calleeName(m))] {
						deferredVerb = true
					}
				}
				return true
			})
			return false
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok || inDefer[n] {
			return false
		}
		switch n := n.(type) {
		case *ast.GoStmt:
			return false
		case *ast.AssignStmt:
			if key, val, ok := boolFieldAssign(pass, n); ok {
				kind := evReset
				if val {
					kind = evSet
				}
				events = append(events, event{pos: n.Pos(), kind: kind, key: key, desc: key})
			}
		case *ast.CallExpr:
			name := calleeName(n)
			switch {
			case canPanicInto(pass, n, params):
				events = append(events, event{pos: n.Pos(), kind: evCall, desc: types.ExprString(n.Fun)})
			case setVerbs[firstWord(name)]:
				events = append(events, event{pos: n.Pos(), kind: evSet, desc: name})
			case resetVerbs[firstWord(name)]:
				events = append(events, event{pos: n.Pos(), kind: evReset, desc: name})
			}
		}
		return true
	})
	reported := map[string]bool{}
	for i, set := range events {
		if set.kind != evSet {
			continue
		}
		var call *event
		for j := i + 1; j < len(events); j++ {
			e := &events[j]
			switch {
			case e.kind == evCall && call == nil:
				call = e
			case e.kind == evReset && call != nil && matches(set, *e):
				if set.key != "" && deferredFlags[set.key] || set.key == "" && deferredVerb {
					break
				}
				id := set.desc + "@" + pass.Fset.Position(set.pos).String()
				if reported[id] {
					break
				}
				reported[id] = true
				pass.Reportf(e.pos, "deferreset: %s undoes %s (line %d) after %s outside a defer; a panic in %s skips it and the state stays set for every later caller: reset in a defer, or recover, reset and re-panic",
					e.desc, set.desc, pass.Fset.Position(set.pos).Line, call.desc, call.desc)
			case e.kind == evReset && call == nil && matches(set, *e):
				// Reset before any panic point: this set is closed.
				j = len(events)
			}
		}
	}
}

// matches pairs a reset with its set: the same flag field, or a verb
// call with a verb call.
func matches(set, reset event) bool {
	return set.key == reset.key
}

// boolFieldAssign recognizes `x.f = true` / `x.f = false` on a bool field.
func boolFieldAssign(pass *analysis.Pass, as *ast.AssignStmt) (key string, val, ok bool) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Tok != token.ASSIGN {
		return "", false, false
	}
	sel, isSel := ast.Unparen(as.Lhs[0]).(*ast.SelectorExpr)
	if !isSel {
		return "", false, false
	}
	s, found := pass.TypesInfo.Selections[sel]
	if !found || s.Kind() != types.FieldVal {
		return "", false, false
	}
	if b, isBasic := s.Type().Underlying().(*types.Basic); !isBasic || b.Kind() != types.Bool {
		return "", false, false
	}
	id, isIdent := ast.Unparen(as.Rhs[0]).(*ast.Ident)
	if !isIdent || (id.Name != "true" && id.Name != "false") {
		return "", false, false
	}
	return types.ExprString(sel), id.Name == "true", true
}

// canPanicInto reports a call that runs code this function does not own:
// a ServeHTTP (the next handler), a Hijack, or a call through a func-typed
// struct field or parameter.
func canPanicInto(pass *analysis.Pass, call *ast.CallExpr, params map[types.Object]bool) bool {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		if fun.Sel.Name == "ServeHTTP" || fun.Sel.Name == "Hijack" {
			return true
		}
		s, ok := pass.TypesInfo.Selections[fun]
		if ok && s.Kind() == types.FieldVal {
			_, isFunc := s.Type().Underlying().(*types.Signature)
			return isFunc
		}
	case *ast.Ident:
		// Only a parameter: a local is the function's own closure or a
		// named function value it picked.
		v, ok := pass.TypesInfo.Uses[fun].(*types.Var)
		if !ok {
			return false
		}
		_, isFunc := v.Type().Underlying().(*types.Signature)
		return isFunc && params[v]
	}
	return false
}

// addParams records the parameters a function signature declares.
func addParams(pass *analysis.Pass, ft *ast.FuncType, out map[types.Object]bool) {
	if ft == nil || ft.Params == nil {
		return
	}
	for _, field := range ft.Params.List {
		for _, id := range field.Names {
			if obj := pass.TypesInfo.Defs[id]; obj != nil {
				out[obj] = true
			}
		}
	}
}

func calleeName(call *ast.CallExpr) string {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// firstWord returns the leading camel-case word of name, lowercased.
func firstWord(name string) string {
	rs := []rune(name)
	end := len(rs)
	for i := 1; i < len(rs); i++ {
		if unicode.IsUpper(rs[i]) {
			end = i
			break
		}
	}
	return strings.ToLower(string(rs[:end]))
}
