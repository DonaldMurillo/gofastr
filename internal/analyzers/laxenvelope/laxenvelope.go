// Package laxenvelope catches a transport that decodes the SAME
// envelope type lax at one site while the same package — or, since the
// 2026-09-07 round, ANY package — decodes that type strictly at
// another.
//
// Probe: core/mcp/stdioenv_red_test.go::TestStdioEnvelopeAmbiguityRefused
// (2026-09-05 round-4 red probes). core/mcp/transport.go decoded the
// JSON-RPC request strictly on the HTTP path (handler.UnmarshalStrict,
// which refuses duplicate and case-folded top-level keys) but served
// the same *Request type on stdio through a bare json.Unmarshal: a
// frame carrying {"method":"safe","method":"danger"} executes the
// second method over stdio while every first-occurrence parser —
// proxy, audit log, WAF — read the first. The strictness contract
// belongs to the TYPE, not to the transport that happens to carry it.
//
// Arms, in order:
//
// Same package (round 4): a type T decoded through
// UnmarshalStrict / DecodeStrict (by name, any package that provides
// one) at one site and through json.Unmarshal /
// (*json.Decoder).Decode at another; the lax site is reported. The fix
// posture is to decode that site through the same strict helper.
//
// Cross package (round 5, arm a): kiln/agent/loop.go's dispatch fed
// the kiln/protocol argument structs through a bare json.Unmarshal
// closure while kiln/chat/server.go's dispatch decoded the SAME types
// through handler.UnmarshalStrict — the contract broken by a package
// that never imports the strict one. go/analysis facts carry the
// strictness mark wherever an import edge exists (ExportObjectFact
// panics on another package's object, so the fact is a PACKAGE fact
// listing the qualified type names). Where no import edge exists —
// the kiln shape — a one-pass source index of the current module
// records every strict decode whose destination declaration names a
// qualified type (var x pkg.T / pkgalias.T), keyed by
// importpath.TypeName. The index reads non-test .go files only,
// skips testdata/ and nested modules, and exists because facts
// physically cannot travel between sibling packages that do not
// import each other.
//
// RawMessage carrier (arm b): when an envelope E is strict-decoded in
// this package and E has a json.RawMessage field, the strict walk
// stopped at the envelope — a lax json.Unmarshal of that field's
// bytes (json.Unmarshal(frame.Params, &p)) is the same ambiguity one
// level down. Probe: core/acp/server.go (wireRequest decoded through
// handler.UnmarshalStrict at the read loop, frame.Params then decoded
// by plain json.Unmarshal in every handler; pinned by
// core/acp/params_strict_red_test.go). Quiet when the package walks
// the field's keys anywhere (handler.CheckObjectKeys on an E.F
// selector — the core/mcp protocol.go chokepoint pattern), or when the
// destination cannot carry key ambiguity at all (a scalar: string,
// number, bool — json.Unmarshal(frame.ID, &id) into an int64 resolves
// nothing).
//
// Top-level-only walk (arm c): handler.CheckTopLevelKeys(b) followed
// in the same function by a json.Unmarshal of b is a walk that
// stopped one level short whenever the destination resolves to a
// struct with a nested struct/map/slice/any field — json.RawMessage
// counts as nested: its bytes are client JSON this walk never saw.
// Probe: framework/experimental/harness/control/mcpserver/server.go
// unmarshalMCPObject (pinned by args_depth_red_test.go); the
// destination is the function's any parameter, so the nesting is read
// at the call sites. Quiet when the same function also runs
// CheckObjectKeys on b, or when every resolved destination is flat
// (all-scalar fields).
//
// Silent postures, deliberately:
//   - types decoded lax here but never strictly anywhere: no
//     strictness contract exists to break — a config file read through
//     json.Unmarshal is not an envelope;
//   - destinations of type any / an interface / a type parameter in a
//     NAMED helper (generic decode helpers like crud's
//     UnmarshalStrict(raw, v) with v any): no concrete type to
//     compare at the decode itself. Arm a resolves the indirection
//     only for FUNCTION LITERALS (kiln's dec closure); arm c resolves
//     it for the CheckTopLevelKeys pairing, where the red probe
//     lives. A named helper whose body just json.Unmarshals into its
//     any parameter is not resolved by arm a — arm c owns that shape
//     when the CheckTopLevelKeys pairing is present;
//   - a RawMessage field handed to a helper before decoding (a2a
//     decodeParams): the helper's own gate is the contract surface;
//   - map destinations in the arm-c pairing (framework/crud decodes
//     its wire-key-folded map itself): the walk there runs with the
//     caller's fold, which is a different contract than depth;
//   - _test.go files on every side: a test's lax decode pins no
//     production contract and must not decide one.
package laxenvelope

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "laxenvelope",
	Doc:  "forbids decoding a type lax (json.Unmarshal / json.Decoder.Decode) that is decoded strictly (UnmarshalStrict/DecodeStrict) elsewhere — in this package, or in any package of the module; plus the RawMessage one-level-down and CheckTopLevelKeys one-level-short shapes",
	Run:  run,
	// FactTypes arms the package-fact channel of the cross-package
	// arm: strict packages re-export the types they decode strictly,
	// and any importer of theirs sees them. The module index below
	// covers the sibling packages no import edge connects.
	FactTypes: []analysis.Fact{(*strictTypesFact)(nil)},
}

const laxMsg = "lax decode of %s, which this package also decodes strictly elsewhere: stdlib json keeps the last duplicate key and folds key case, so a first-occurrence parser reads a different request than this dispatcher runs — decode this site through the strict helper too"

const crossLaxMsg = "lax decode of %s, which %s also decodes strictly (UnmarshalStrict/DecodeStrict): the no-ambiguity contract belongs to the type, not the package that happens to decode strictly — decode this site through the strict helper too"

const crossLaxManyMsg = "lax decode into %d types that other packages of this module decode strictly (UnmarshalStrict/DecodeStrict), starting with %s from %s: the no-ambiguity contract belongs to each type — decode this site through the strict helper too"

const rawMsg = "lax decode of %s, a json.RawMessage field of %s, which this package decodes strictly as an envelope: the strict walk stopped at the envelope, so stdlib's last-key-wins runs on the payload one level down — walk these keys too (handler.CheckObjectKeys) or decode through the strict helper"

const topMsg = "json.Unmarshal of %s after only handler.CheckTopLevelKeys in %s: the walk stopped one level short — the destination (or a call site's destination, via the any parameter) has nested objects whose duplicate and case-folded keys resolve last-wins; use handler.CheckObjectKeys, which walks every depth"

// strictTypesFact is the package fact listing qualified type names
// ("import/path.TypeName") that the exporting package decodes through
// UnmarshalStrict/DecodeStrict. Object facts cannot carry it: the
// strict package does not own the types it decodes.
type strictTypesFact struct{ Types []string }

func (strictTypesFact) AFact() {}

// moduleStrictIndex is the process-wide, per-module-root index of
// qualified type names strict-decoded somewhere in the module. Built
// lazily, once per root: every package's analysis pass in a single
// driver process shares it. The unitchecker runs one process per
// package, so each pays one build — the keyword pre-filter keeps that
// at reading bytes, not parsing, for all but the ~30 files that name a
// strict decoder.
var (
	indexOnce sync.Map // module root dir → *strictIndex
)

type strictIndex struct {
	strict map[string]string // "import/path.TypeName" → package path that decodes it strictly
}

func run(pass *analysis.Pass) (any, error) {
	files := prodFiles(pass)

	// Arm b and arm c bookkeeping.
	envelopes := envelopeTypes(pass, files)  // type → set of RawMessage field names
	rawCredit := rawFieldCredit(pass, files) // "type.Field" → true (CheckObjectKeys walked it)

	// Same-package strict set (arm 1) and arm-c positions.
	strict := map[types.Type]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			fn, ok := calleeFunc(pass, call)
			if !ok {
				return true
			}
			switch fn.Name() {
			case "UnmarshalStrict", "DecodeStrict":
				if t, ok := dstType(pass, call.Args[1]); ok {
					strict[t] = true
				}
			}
			return true
		})
	}

	// Cross-package strict knowledge from facts first (import edges are
	// free); the module index below is the expensive fallback.
	cross := map[string]string{} // qualified name → strict package path
	for _, imp := range pass.Pkg.Imports() {
		var fact strictTypesFact
		if pass.ImportPackageFact(imp, &fact) {
			for _, q := range fact.Types {
				if _, ok := cross[q]; !ok {
					cross[q] = imp.Path()
				}
			}
		}
	}

	// Export our own strict decodes for importers.
	if len(strict) > 0 {
		var qs []string
		for t := range strict {
			if q, ok := qualifiedName(t, pass.Pkg); ok {
				qs = append(qs, q)
			}
		}
		if len(qs) > 0 {
			pass.ExportPackageFact(&strictTypesFact{Types: qs})
		}
	}

	// Function literals wrapping a lax decode over an any parameter:
	// the concrete type is only visible at the closure's call sites
	// (kiln's dec closure).
	litDecodes := closureLaxDecodes(pass, files)

	// The module index re-walks the whole module reading every non-test
	// .go file, and `go vet -vettool` runs one process per package, so
	// it must not run for a package with nothing to look up: build it
	// only when some lax decode here targets a named type from another
	// package that neither this package nor an imported fact already
	// proves strict.
	if hasUnresolvedCandidates(pass, files, strict, cross, litDecodes) {
		if idx := moduleIndex(pass, files); idx != nil {
			for q, pkg := range idx.strict {
				if _, ok := cross[q]; !ok {
					cross[q] = pkg
				}
			}
		}
	}

	reported := map[token.Pos]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Arms 1/a: lax decode into a type that is strict here or
			// strictly decoded by another package.
			if dst, lax := laxDecodeDst(pass, call); lax {
				if t, ok := dstType(pass, dst); ok {
					if strict[t] {
						if !reported[call.Pos()] {
							reported[call.Pos()] = true
							pass.Reportf(call.Pos(), laxMsg, typeLabel(pass, t))
						}
						return true
					}
					if q, ok := qualifiedName(t, pass.Pkg); ok {
						if srcPkg, isCross := cross[q]; isCross {
							reportCross(pass, call.Pos(), []strictEntry{{srcPkg, t}})
							reported[call.Pos()] = true
						}
					}
					return true
				}
				// The destination is the any parameter of a function
				// literal wrapping this decode (kiln's dec closure):
				// the concrete types live at the literal's call sites.
				if entry, ok := litDecodes[call.Pos()]; ok {
					var hits []strictEntry
					for _, t := range entry.types {
						if strict[t] {
							hits = append(hits, strictEntry{"", t})
							continue
						}
						if q, ok := qualifiedName(t, pass.Pkg); ok {
							if srcPkg, ok := cross[q]; ok {
								hits = append(hits, strictEntry{srcPkg, t})
							}
						}
					}
					if len(hits) > 0 {
						reportCross(pass, call.Pos(), hits)
						reported[call.Pos()] = true
					}
				}
				return true
			}
			return true
		})
	}

	armB(pass, files, envelopes, rawCredit)
	armC(pass, files)
	return nil, nil
}

// hasUnresolvedCandidates reports whether any lax decode in the
// package targets a named type from another package of this module
// whose strictness is not already known (a same-package strict decode
// or an imported fact) — the only case the module index can settle.
// Types from outside the module can never be strict-decoded by it, so
// they never trigger the walk.
func hasUnresolvedCandidates(pass *analysis.Pass, files []*ast.File, strict map[types.Type]bool, cross map[string]string, litDecodes map[token.Pos]*closureEntry) bool {
	if len(files) == 0 {
		return false
	}
	_, modPath, _ := moduleRoot(filepath.Dir(pass.Fset.Position(files[0].Pos()).Filename))
	inModule := func(q string) bool {
		return modPath != "" && (q == modPath || strings.HasPrefix(q, modPath+"/"))
	}
	unresolved := func(t types.Type) bool {
		if strict[t] {
			return false
		}
		q, ok := qualifiedName(t, pass.Pkg)
		return ok && inModule(q) && cross[q] == ""
	}
	for _, f := range files {
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			dst, lax := laxDecodeDst(pass, call)
			if !lax {
				return true
			}
			if t, ok := dstType(pass, dst); ok {
				if unresolved(t) {
					found = true
					return false
				}
				return true
			}
			if entry, ok := litDecodes[call.Pos()]; ok {
				for _, t := range entry.types {
					if unresolved(t) {
						found = true
						return false
					}
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

type strictEntry struct {
	qualified string
	typ       types.Type
}

func reportCross(pass *analysis.Pass, pos token.Pos, hits []strictEntry) {
	if len(hits) == 1 {
		h := hits[0]
		if h.qualified == "" { // strict in this same package, via closure
			pass.Reportf(pos, laxMsg, typeLabel(pass, h.typ))
			return
		}
		pass.Reportf(pos, crossLaxMsg, typeLabel(pass, h.typ), h.qualified)
		return
	}
	first := hits[0]
	pass.Reportf(pos, crossLaxManyMsg, len(hits), typeLabel(pass, first.typ), first.qualified)
}

func prodFiles(pass *analysis.Pass) []*ast.File {
	var out []*ast.File
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// qualifiedName renders t as "importpath.TypeName" for named types
// from another package; own-package and non-named types report false.
func qualifiedName(t types.Type, self *types.Package) (string, bool) {
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return "", false
	}
	if named.Obj().Pkg() == self {
		return "", false
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name(), true
}

// ---- arm a: closure-wrapped lax decodes ------------------------------

// closureEntry is one json.Unmarshal inside a function literal whose
// destination is the literal's own any-parameter, with the concrete
// types observed at the literal's call sites.
type closureEntry struct {
	types []types.Type
}

// closureLaxDecodes finds `dec := func(out any) error { ...
// json.Unmarshal(b, out) ... }` and resolves dec(&x) call sites into
// concrete types. Only literals assigned to a plain identifier in the
// same file, and only calls through that identifier, are resolved —
// the kiln shape. Anything more elaborate stays quiet.
func closureLaxDecodes(pass *analysis.Pass, files []*ast.File) map[token.Pos]*closureEntry {
	out := map[token.Pos]*closureEntry{}
	for _, f := range files {
		lits := map[*types.Var]*ast.FuncLit{}
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			id, ok := assign.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			lit, ok := assign.Rhs[0].(*ast.FuncLit)
			if !ok {
				return true
			}
			if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
				lits[v] = lit
			}
			return true
		})
		if len(lits) == 0 {
			continue
		}
		for v, lit := range lits {
			// Parameters by object identity.
			params := map[types.Object]bool{}
			for _, field := range lit.Type.Params.List {
				for _, name := range field.Names {
					if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
						params[obj] = true
					}
				}
			}
			// Lax decodes over one of those parameters.
			decodeArg := map[token.Pos]int{} // lax call pos → param index
			ast.Inspect(lit.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				dst, lax := laxDecodeDst(pass, call)
				if !lax {
					return true
				}
				id, ok := dst.(*ast.Ident)
				if !ok {
					return true
				}
				obj := pass.TypesInfo.ObjectOf(id)
				if !params[obj] {
					return true
				}
				idx := 0
				for _, field := range lit.Type.Params.List {
					for _, name := range field.Names {
						if pass.TypesInfo.ObjectOf(name) == obj {
							decodeArg[call.Pos()] = idx
						}
						idx++
					}
				}
				return true
			})
			if len(decodeArg) == 0 {
				continue
			}
			entry := &closureEntry{}
			seen := map[types.Type]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if pass.TypesInfo.ObjectOf(id) != v {
					return true
				}
				for _, idx := range decodeArg {
					if idx >= len(call.Args) {
						continue
					}
					if t, ok := dstType(pass, call.Args[idx]); ok && !seen[t] {
						seen[t] = true
						entry.types = append(entry.types, t)
					}
				}
				return true
			})
			for pos := range decodeArg {
				if len(entry.types) > 0 {
					out[pos] = entry
				}
			}
		}
	}
	return out
}

// ---- arm b: envelope RawMessage fields -------------------------------

// envelopeTypes returns the set of types this package strict-decodes
// that carry at least one json.RawMessage field, mapped to those field
// names.
func envelopeTypes(pass *analysis.Pass, files []*ast.File) map[types.Type]map[string]bool {
	strict := map[types.Type]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			fn, ok := calleeFunc(pass, call)
			if !ok {
				return true
			}
			if fn.Name() == "UnmarshalStrict" || fn.Name() == "DecodeStrict" {
				if t, ok := dstType(pass, call.Args[1]); ok {
					strict[t] = true
				}
			}
			return true
		})
	}
	out := map[types.Type]map[string]bool{}
	for t := range strict {
		named, ok := types.Unalias(t).(*types.Named)
		if !ok {
			continue
		}
		s, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := range s.NumFields() {
			if isRawMessage(s.Field(i).Type()) {
				if out[t] == nil {
					out[t] = map[string]bool{}
				}
				out[t][s.Field(i).Name()] = true
			}
		}
	}
	return out
}

// isRawMessage recognizes json.RawMessage as a field or destination
// type. Go 1.27's encoding/json v2 spells it an alias
// (type RawMessage = jsontext.Value), so the alias arm is not optional.
func isRawMessage(t types.Type) bool {
	if alias, ok := t.(*types.Alias); ok {
		obj := alias.Obj()
		return obj != nil && obj.Pkg() != nil &&
			obj.Pkg().Path() == "encoding/json" && obj.Name() == "RawMessage"
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj() != nil && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "encoding/json" && named.Obj().Name() == "RawMessage"
}

// rawFieldCredit records "TypeName.Field" pairs whose bytes this
// package walks with handler.CheckObjectKeys (the core/mcp chokepoint:
// one check at the dispatcher covers every downstream sub-decode).
func rawFieldCredit(pass *analysis.Pass, files []*ast.File) map[string]bool {
	credit := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 1 {
				return true
			}
			fn, ok := calleeFunc(pass, call)
			if !ok || fn.Name() != "CheckObjectKeys" {
				return true
			}
			sel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				return true
			}
			base := pass.TypesInfo.TypeOf(sel.X)
			if base == nil {
				return true
			}
			if p, ok := base.(*types.Pointer); ok {
				base = p.Elem()
			}
			named, ok := types.Unalias(base).(*types.Named)
			if !ok {
				return true
			}
			credit[named.Obj().Name()+"."+sel.Sel.Name] = true
			return true
		})
	}
	return credit
}

func armB(pass *analysis.Pass, files []*ast.File, envelopes map[types.Type]map[string]bool, credit map[string]bool) {
	if len(envelopes) == 0 {
		return
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			dst, lax := laxDecodeDst(pass, call)
			if !lax {
				return true
			}
			// The bytes argument must be an envelope's RawMessage field.
			sel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				return true
			}
			base := pass.TypesInfo.TypeOf(sel.X)
			if base == nil {
				return true
			}
			if p, ok := base.(*types.Pointer); ok {
				base = p.Elem()
			}
			named, ok := types.Unalias(base).(*types.Named)
			if !ok {
				return true
			}
			fields, ok := envelopes[named]
			if !ok || !fields[sel.Sel.Name] {
				return true
			}
			if credit[named.Obj().Name()+"."+sel.Sel.Name] {
				return true
			}
			// A destination that cannot carry key ambiguity resolves
			// nothing: json.Unmarshal(frame.ID, &id) into an int64 is
			// quiet. Composite and any destinations resolve keys.
			if !compositeDst(pass, dst) {
				return true
			}
			pass.Reportf(call.Pos(), rawMsg, sel.Sel.Name, typeLabel(pass, named))
			return true
		})
	}
}

// compositeDst reports whether the decode destination can resolve
// object keys: a struct, map, slice, array, or any/interface.
func compositeDst(pass *analysis.Pass, dst ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(dst)
	if t == nil {
		return false
	}
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Struct, *types.Map, *types.Slice, *types.Array:
		return true
	case *types.Basic:
		return false
	case *types.Chan, *types.Signature:
		return false
	default:
		_ = u
		return false
	}
}

// ---- arm c: CheckTopLevelKeys one level short -------------------------

func armC(pass *analysis.Pass, files []*ast.File) {
	for _, f := range files {
		// Call sites of package functions, for resolving any-dst.
		callSites := map[*types.Func][]*ast.CallExpr{}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if fn, ok := calleeFunc(pass, call); ok && fn.Pkg() == pass.Pkg {
				callSites[fn] = append(callSites[fn], call)
			}
			return true
		})
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			checks := map[types.Object]token.Pos{} // var → CheckTopLevelKeys pos
			covered := map[types.Object]bool{}     // var → CheckObjectKeys walked it
			unmarshals := map[types.Object][]ast.Expr{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fn, ok := calleeFunc(pass, call)
				if !ok {
					return true
				}
				switch fn.Name() {
				case "CheckTopLevelKeys", "CheckObjectKeys":
					if len(call.Args) < 1 {
						return true
					}
					id, ok := call.Args[0].(*ast.Ident)
					if !ok {
						return true
					}
					obj := pass.TypesInfo.ObjectOf(id)
					if obj == nil {
						return true
					}
					if fn.Name() == "CheckTopLevelKeys" {
						checks[obj] = call.Pos()
					} else {
						covered[obj] = true
					}
				case "Unmarshal":
					dst, lax := laxDecodeDst(pass, call)
					if !lax || len(call.Args) < 1 {
						return true
					}
					if fn := laxUnmarshalFunc(pass, call); fn == nil {
						return true
					}
					id, ok := call.Args[0].(*ast.Ident)
					if !ok {
						return true
					}
					obj := pass.TypesInfo.ObjectOf(id)
					if obj == nil {
						return true
					}
					unmarshals[obj] = append(unmarshals[obj], dst)
				}
				return true
			})
			for obj, dsts := range unmarshals {
				checkPos, checked := checks[obj]
				if !checked || covered[obj] {
					continue
				}
				for _, dst := range dsts {
					if nestedDestination(pass, dst, callSites) {
						pass.Reportf(checkPos, topMsg, obj.Name(), fd.Name.Name)
						break
					}
				}
			}
		}
	}
}

// laxUnmarshalFunc returns the *types.Func when call is a plain
// json.Unmarshal (not a Decoder.Decode, whose bytes argument is a
// reader, not the checked bytes).
func laxUnmarshalFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "encoding/json" || fn.Name() != "Unmarshal" {
		return nil
	}
	return fn
}

// nestedDestination reports whether dst (or, when dst is an any
// parameter of the enclosing function, any argument passed at the
// function's package-local call sites) is a struct with a field whose
// value stdlib json resolves keys inside: struct, map, slice, array,
// any/interface — or json.RawMessage, whose bytes are client JSON the
// top-level walk never saw.
func nestedDestination(pass *analysis.Pass, dst ast.Expr, callSites map[*types.Func][]*ast.CallExpr) bool {
	// Direct concrete destination.
	if t := pass.TypesInfo.TypeOf(dst); t != nil {
		if nestedType(pass, t) {
			return true
		}
	}
	// any-parameter: resolve at call sites. dst must be a plain ident.
	id, ok := dst.(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if obj == nil {
		return false
	}
	for fn, sites := range callSites {
		if fn.Name() == "" || fn.Pkg() != pass.Pkg {
			continue
		}
		idx := -1
		params := fn.Signature().Params()
		for j := range params.Len() {
			if params.At(j) == obj {
				idx = j
			}
		}
		if idx < 0 {
			continue
		}
		for _, site := range sites {
			if idx < len(site.Args) {
				if t := pass.TypesInfo.TypeOf(site.Args[idx]); t != nil && nestedType(pass, t) {
					return true
				}
			}
		}
	}
	return false
}

func nestedType(pass *analysis.Pass, t types.Type) bool {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	if isRawMessage(t) {
		return true
	}
	// Anonymous structs count: mcpserver's call sites decode into
	// struct{ Name string; Arguments json.RawMessage }.
	s, ok := types.Unalias(t).Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range s.NumFields() {
		ft := s.Field(i).Type()
		for {
			p, ok := ft.(*types.Pointer)
			if !ok {
				break
			}
			ft = p.Elem()
		}
		switch u := ft.Underlying().(type) {
		case *types.Interface, *types.Struct, *types.Map, *types.Slice, *types.Array:
			// A struct field that json decodes through its own
			// UnmarshalJSON (time.Time) resolves no keys itself.
			if _, isStruct := u.(*types.Struct); isStruct && implementsUnmarshaler(ft) {
				continue
			}
			// json.RawMessage is client JSON this walk never saw.
			if isRawMessage(ft) {
				return true
			}
			// A slice of scalars resolves no keys; of composites does.
			if sl, ok := u.(*types.Slice); ok && !compositeElem(pass, sl.Elem()) {
				continue
			}
			if ar, ok := u.(*types.Array); ok && !compositeElem(pass, ar.Elem()) {
				continue
			}
			return true
		}
	}
	return false
}

// compositeElem reports whether a slice/array element can carry an
// object (struct, map, any, or itself a composite slice).
func compositeElem(pass *analysis.Pass, t types.Type) bool {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	switch t.Underlying().(type) {
	case *types.Interface, *types.Struct, *types.Map:
		return true
	case *types.Slice, *types.Array:
		return compositeElem(pass, t.Underlying())
	}
	return false
}

// implementsUnmarshaler reports whether t (or *t) declares an
// UnmarshalJSON([]byte) error method: stdlib json hands such a type its
// bytes verbatim, so no key resolution happens inside it (time.Time).
func implementsUnmarshaler(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil {
		return false
	}
	ms := types.NewMethodSet(types.NewPointer(named))
	for i := range ms.Len() {
		m := ms.At(i).Obj()
		if m.Name() != "UnmarshalJSON" {
			continue
		}
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			continue
		}
		if sig.Params().Len() == 1 && sig.Results().Len() == 1 &&
			sig.Params().At(0).Type().String() == "[]byte" &&
			sig.Results().At(0).Type().String() == "error" {
			return true
		}
	}
	return false
}

// ---- module index -----------------------------------------------------

// moduleIndex returns the strictness index for the module containing
// the analyzed files, or nil when there is no module (GOPATH fixtures)
// or the files live under testdata/.
func moduleIndex(pass *analysis.Pass, files []*ast.File) *strictIndex {
	if len(files) == 0 {
		return nil
	}
	first := pass.Fset.Position(files[0].Pos()).Filename
	if strings.Contains(first, "/testdata/") {
		return nil
	}
	root, modPath, ok := moduleRoot(filepath.Dir(first))
	if !ok {
		return nil
	}
	if cached, ok := indexOnce.Load(root); ok {
		return cached.(*strictIndex)
	}
	idx := buildIndex(root, modPath)
	actual, _ := indexOnce.LoadOrStore(root, idx)
	return actual.(*strictIndex)
}

// moduleRoot walks up from dir to the nearest go.mod, returning the
// module root and its path declaration.
func moduleRoot(dir string) (root, modPath string, ok bool) {
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if rest, found := strings.CutPrefix(line, "module "); found {
					return dir, strings.TrimSpace(rest), true
				}
			}
			return dir, "", true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

func buildIndex(root, modPath string) *strictIndex {
	idx := &strictIndex{strict: map[string]string{}}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || name == "node_modules" ||
				strings.HasPrefix(name, ".") || name == "dist") {
				return fs.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return fs.SkipDir // nested module: different path space
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !strictKeyword(data) {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, data, 0)
		if perr != nil {
			return nil
		}
		collectStrictCalls(f, dirImportPath(root, modPath, filepath.Dir(path)), idx)
		return nil
	})
	return idx
}

func strictKeyword(data []byte) bool {
	return bytesContains(data, []byte("UnmarshalStrict")) || bytesContains(data, []byte("DecodeStrict"))
}

func bytesContains(hay, needle []byte) bool {
	return strings.Contains(string(hay), string(needle))
}

func dirImportPath(root, modPath, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return modPath
	}
	return modPath + "/" + filepath.ToSlash(rel)
}

// collectStrictCalls indexes UnmarshalStrict/DecodeStrict calls whose
// destination resolves, through file-local declarations, to a
// qualified type name: var x pkg.T (or pkgalias.T), the file's own
// package for bare T, or a composite-literal initializer.
func collectStrictCalls(f *ast.File, ownPath string, idx *strictIndex) {
	// import alias/name → import path.
	imports := map[string]string{}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		} else {
			parts := strings.Split(path, "/")
			name = parts[len(parts)-1]
		}
		if name != "_" && name != "." {
			imports[name] = path
		}
	}
	// Name → qualified type, resolved per function: declarations are
	// local (kiln/chat declares `var args protocol.WorldGetArgs` inside
	// the dispatch switch), and a file-scoped map would glue two
	// functions' same-named locals together.
	types_ := map[string]string{}
	record := func(names []*ast.Ident, typ ast.Expr) {
		if typ == nil {
			return
		}
		if q := qualifyTypeExpr(typ, imports, ownPath); q != "" {
			for _, name := range names {
				types_[name.Name] = q
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			clear(types_)
			if v.Type != nil {
				for _, field := range v.Type.Params.List {
					record(field.Names, field.Type)
				}
				if v.Type.Results != nil {
					for _, field := range v.Type.Results.List {
						record(field.Names, field.Type)
					}
				}
			}
		case *ast.FuncLit:
			clear(types_)
			if v.Type != nil {
				for _, field := range v.Type.Params.List {
					record(field.Names, field.Type)
				}
			}
		case *ast.GenDecl:
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if vs.Type != nil {
					record(vs.Names, vs.Type)
				} else if len(vs.Values) == 1 {
					if lit, ok := vs.Values[0].(*ast.CompositeLit); ok && lit.Type != nil {
						record(vs.Names, lit.Type)
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				if i >= len(v.Rhs) {
					break
				}
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				lit, ok := v.Rhs[i].(*ast.CompositeLit)
				if !ok || lit.Type == nil {
					continue
				}
				record([]*ast.Ident{id}, lit.Type)
			}
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "UnmarshalStrict" && sel.Sel.Name != "DecodeStrict") {
				return true
			}
			// Only the two-arg decode shapes carry a destination.
			if len(v.Args) != 2 {
				return true
			}
			dst := v.Args[1]
			if u, ok := dst.(*ast.UnaryExpr); ok && u.Op == token.AND {
				dst = u.X
			}
			id, ok := dst.(*ast.Ident)
			if !ok {
				return true
			}
			if q, ok := types_[id.Name]; ok && q != "" {
				if _, exists := idx.strict[q]; !exists {
					idx.strict[q] = ownPath
				}
			}
		}
		return true
	})
}

// qualifyTypeExpr renders `pkgalias.T` / `T` as an import-qualified
// name, or "" when the expression is not a plain (optionally pointer)
// named type.
func qualifyTypeExpr(e ast.Expr, imports map[string]string, ownPath string) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkgID, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	path, ok := imports[pkgID.Name]
	if !ok {
		return ""
	}
	return path + "." + sel.Sel.Name
}

// laxDecodeDst recognizes the lax decode spellings — json.Unmarshal
// and a Decode method on a *json.Decoder — returning the destination
// argument.
func laxDecodeDst(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func); ok && len(call.Args) == 2 {
		if fn.Pkg() != nil && fn.Pkg().Path() == "encoding/json" && fn.Name() == "Unmarshal" {
			return call.Args[1], true
		}
		return nil, false
	}
	if sel.Sel.Name != "Decode" || len(call.Args) != 1 {
		return nil, false
	}
	recv, ok := pass.TypesInfo.Types[sel.X]
	if !ok || recv.Type == nil {
		return nil, false
	}
	if p, ok := recv.Type.(*types.Pointer); ok {
		if named, ok := p.Elem().(*types.Named); ok && named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "encoding/json" && named.Obj().Name() == "Decoder" {
			return call.Args[0], true
		}
	}
	return nil, false
}

// dstType resolves the decoded type: &v and v (already a pointer) both
// give T. any/interface/type-parameter destinations have no concrete
// type to compare.
func dstType(pass *analysis.Pass, arg ast.Expr) (types.Type, bool) {
	t := pass.TypesInfo.TypeOf(arg)
	if t == nil {
		return nil, false
	}
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	switch t.(type) {
	case *types.Interface, *types.TypeParam:
		return nil, false
	}
	if _, ok := t.Underlying().(*types.Interface); ok {
		return nil, false
	}
	return t, true
}

func calleeFunc(pass *analysis.Pass, call *ast.CallExpr) (*types.Func, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		fn, ok := pass.TypesInfo.ObjectOf(fun).(*types.Func)
		return fn, ok
	case *ast.SelectorExpr:
		fn, ok := pass.TypesInfo.Uses[fun.Sel].(*types.Func)
		return fn, ok
	}
	return nil, false
}

func typeLabel(pass *analysis.Pass, t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == pass.Pkg {
			return ""
		}
		return p.Name()
	})
}

func isTest(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}
