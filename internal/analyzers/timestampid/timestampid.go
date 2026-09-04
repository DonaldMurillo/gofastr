// Package timestampid catches an identifier minted from wall-clock
// time: a time.Now().Unix()/.UnixMilli()/.UnixNano() value formatted
// into a string (fmt.Sprintf, strconv.FormatInt/FormatUint/Itoa, or +
// concatenation) and bound to a name ending in id, token, session,
// key, nonce, secret, or capability.
//
// The bug class: a wall-clock id has zero unpredictable bits, so a
// second client that can reach the surface enumerates it without ever
// minting one. Probe TestAcpSessionRedUnpredictableID (2026-09-03
// adversarial pass round 5) pinned kiln/acp/acp.go NewSession minting
// fmt.Sprintf("kiln-%d-%d", time.Now().UnixNano(), counter) —
// session/load treats the bare ID as the replay capability for the
// whole journaled conversation. The same shape mints the kiln chat
// journal entry/call ids (kiln/chat/server.go applyEntry and
// nextCallID, kiln/protocol/protocol.go nextEntryID) and the ACP
// message ids (acp.go messageID). Every credential the rest of the
// repo mints draws >=128 bits from crypto/rand
// (framework/experimental/harness/ids, battery/auth token helpers).
// All of these bugs are still open.
//
// The rule fires when the formatted wall-clock value is bound to a
// credential-shaped name: a variable or struct field whose last
// camelCase word is one of the seven suffixes above, or a return from
// a function so named.
//
// Silent postures, deliberately:
//   - timestamps that are values, not identifiers: names like now,
//     createdAt, sentAt, or a filename never match the suffix set, and
//     an unformatted int64 (createdAt := time.Now().UnixMilli()) or a
//     time.Time stored in a field has no string minting at all;
//   - ids whose same expression also includes >=16 bytes of crypto/rand
//     (the rand.Read buffer feeding the format, the ULID/harness-ids
//     posture): the entropy is real. Fewer bytes (an 8-byte
//     collision-defeater) do not silence anything;
//   - a clock passed as a parameter (now time.Time): the injectable
//     clock of ulid.NewAt and the test seams; only time.Now() itself
//     is wall-clock here;
//   - names in the observability family — buildID, logID, traceID,
//     spanID, requestID, reqID, runID, batchID, debugID, devID — label
//     work for humans; guessing them grants nothing;
//   - crypto/rand-failure fallbacks are NOT silent: the fallback branch
//     mints the enumerable id precisely when randomness is gone, which
//     is the worst moment for it (battery/auth generateAPITokenID and
//     generateUserID, core/stream generateSubscriberID and
//     randomConnectionID);
//   - _test.go files.
package timestampid

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "timestampid",
	Doc:  "forbids minting an id/token/session/key-named identifier from time.Now().Unix* (enumerable capability); mint from crypto/rand like the ids package",
	Run:  run,
}

const maxDepth = 4

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, fn)
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl) {
	bound := bindings(pass, fn.Body)
	bigRand := bigRandBuffers(pass, fn.Body, bound)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
				return true
			}
			name, ok := bindingName(st.Lhs[0])
			if !ok || !capabilityName(name) {
				return true
			}
			if minted(pass, st.Rhs[0], bound, bigRand, 0) {
				report(pass, st.Pos(), name)
			}
		case *ast.ReturnStmt:
			if !capabilityName(fn.Name.Name) {
				return true
			}
			for _, r := range st.Results {
				if minted(pass, r, bound, bigRand, 0) {
					report(pass, st.Pos(), fn.Name.Name)
				}
			}
		}
		return true
	})
}

func report(pass *analysis.Pass, pos token.Pos, name string) {
	pass.Reportf(pos, "%s minted from wall-clock time is enumerable; mint from crypto/rand (ids package)", name)
}

// bindingName returns the name a mint is bound to: the variable for an
// identifier, the field for a selector (e.ID = ...).
func bindingName(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name, v.Name != "_"
	case *ast.SelectorExpr:
		return v.Sel.Name, true
	}
	return "", false
}

// capabilitySuffixes are the trailing words that make a name an
// identifier of a capability, session, or entry.
var capabilitySuffixes = map[string]bool{
	"id": true, "token": true, "session": true, "key": true,
	"nonce": true, "secret": true, "capability": true,
}

// observabilityWords precede "ID" in names that label builds, logs,
// traces, and requests: developer labels, not bearer capabilities.
var observabilityWords = map[string]bool{
	"build": true, "log": true, "trace": true, "span": true,
	"request": true, "req": true, "run": true, "batch": true,
	"debug": true, "dev": true,
}

// capabilityName reports whether the binding name ends in a
// capability suffix word: id, entryID, nextCallID, sessionToken. A
// preceding observability word (buildID, traceID) removes it.
func capabilityName(name string) bool {
	words := splitWords(name)
	if len(words) == 0 {
		return false
	}
	last := strings.ToLower(words[len(words)-1])
	if !capabilitySuffixes[last] {
		return false
	}
	if last == "id" && len(words) >= 2 && observabilityWords[strings.ToLower(words[len(words)-2])] {
		return false
	}
	return true
}

// minted reports whether e is a string mint whose assembly includes a
// wall-clock value: a fmt.Sprintf / strconv format call or a +
// concatenation mentioning time.Now().Unix*() (directly or through a
// local), and no >=16-byte crypto/rand buffer in the same assembly.
func minted(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, bigRand map[types.Object]bool, depth int) bool {
	if depth > maxDepth {
		return false
	}
	if mentionsBigRand(pass, e, bigRand, depth) {
		return false
	}
	switch v := e.(type) {
	case *ast.CallExpr:
		q := qualifiedFunc(pass, v.Fun)
		switch q {
		case "fmt.Sprintf", "strconv.FormatInt", "strconv.FormatUint", "strconv.Itoa":
			for _, a := range v.Args {
				if mentionsWallClock(pass, a, bound, depth) {
					return true
				}
			}
		}
		return false
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return false
		}
		return mentionsWallClock(pass, v.X, bound, depth) || minted(pass, v.Y, bound, bigRand, depth+1)
	}
	return false
}

// mentionsWallClock reports whether e's assembly involves a wall-clock
// reading: time.Now() followed by Unix/UnixMilli/UnixMicro/UnixNano,
// directly or through a local bound to time.Now().
func mentionsWallClock(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) bool {
	if depth > maxDepth {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && depth < maxDepth {
			if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok && b != nil {
				if mentionsWallClock(pass, b, bound, depth+1) {
					found = true
					return false
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Unix", "UnixMilli", "UnixMicro", "UnixNano":
		default:
			return true
		}
		if isNow(pass, sel.X, bound, depth) {
			found = true
		}
		return !found
	})
	return found
}

// isNow reports whether e is time.Now() itself or a local whose
// binding is time.Now(). A clock passed as a parameter is the
// injectable-clock posture and is not wall-clock here.
func isNow(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) bool {
	if call, ok := e.(*ast.CallExpr); ok && qualifiedFunc(pass, call.Fun) == "time.Now" {
		return true
	}
	if id, ok := e.(*ast.Ident); ok && depth < maxDepth {
		if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok && b != nil {
			return isNow(pass, b, bound, depth+1)
		}
	}
	return false
}

// mentionsBigRand reports whether e's assembly (through locals) reads
// from a buffer holding >=16 bytes of crypto/rand.
func mentionsBigRand(pass *analysis.Pass, e ast.Expr, bigRand map[types.Object]bool, depth int) bool {
	if len(bigRand) == 0 || depth > maxDepth {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		var obj types.Object
		switch v := n.(type) {
		case *ast.Ident:
			obj = pass.TypesInfo.ObjectOf(v)
		case *ast.SliceExpr:
			if id, ok := v.X.(*ast.Ident); ok {
				obj = pass.TypesInfo.ObjectOf(id)
			}
		case *ast.IndexExpr:
			if id, ok := v.X.(*ast.Ident); ok {
				obj = pass.TypesInfo.ObjectOf(id)
			}
		}
		if obj != nil && bigRand[obj] {
			found = true
			return false
		}
		return !found
	})
	return found
}

// bigRandBuffers collects the buffers this function fills from
// crypto/rand with >=16 bytes: the entropy that makes an id
// unguessable. Smaller buffers (the 8-byte collision defeaters) do
// not collect.
func bigRandBuffers(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr) map[types.Object]bool {
	var big map[types.Object]bool
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if qualifiedFunc(pass, call.Fun) != "crypto/rand.Read" {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		arg := call.Args[0]
		if se, ok := arg.(*ast.SliceExpr); ok {
			arg = se.X
		}
		id, ok := arg.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.ObjectOf(id)
		if obj == nil {
			return true
		}
		if bufSize(pass, obj, bound) >= 16 {
			if big == nil {
				big = map[types.Object]bool{}
			}
			big[obj] = true
		}
		return true
	})
	return big
}

// bufSize returns the declared byte size of a rand.Read buffer: the N
// of make([]byte, N) or var b [N]byte, or -1 when it cannot be read.
func bufSize(pass *analysis.Pass, obj types.Object, bound map[types.Object]ast.Expr) int {
	if b, ok := bound[obj]; ok && b != nil {
		if call, ok := b.(*ast.CallExpr); ok && qualifiedFunc(pass, call.Fun) == "make" {
			if len(call.Args) == 2 || len(call.Args) == 3 {
				if tv, ok := pass.TypesInfo.Types[call.Args[1]]; ok && tv.Value != nil {
					if n, ok := constant.Int64Val(tv.Value); ok {
						return int(n)
					}
				}
			}
		}
	}
	if v, ok := obj.(*types.Var); ok && v.Type() != nil {
		if arr, ok := v.Type().Underlying().(*types.Array); ok {
			return int(arr.Len())
		}
	}
	return -1
}

// bindings maps each local defined by an assignment or value
// declaration to the expression it was last bound to.
func bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	m := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" && i < len(st.Rhs) {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						m[obj] = st.Rhs[i]
					}
				}
			}
		case *ast.GenDecl:
			if st.Tok != token.VAR {
				return true
			}
			for _, spec := range st.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if name.Name == "_" || i >= len(vs.Values) {
						continue
					}
					if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
						m[obj] = vs.Values[i]
					}
				}
			}
		}
		return true
	})
	return m
}

// qualifiedFunc renders a selector callee as "importpath.Func",
// resolving the package through the type checker so aliased imports
// still match.
func qualifiedFunc(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkgName, ok := pass.TypesInfo.ObjectOf(id).(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Path() + "." + sel.Sel.Name
}

// splitWords splits an identifier into camelCase / underscore words.
func splitWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		switch {
		case cur == '_' || !unicode.IsLetter(cur) && !unicode.IsDigit(cur):
			if start < i {
				words = append(words, string(runes[start:i]))
			}
			start = i + 1
		case unicode.IsUpper(cur) && unicode.IsLower(prev),
			unicode.IsUpper(cur) && unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
			if start < i {
				words = append(words, string(runes[start:i]))
			}
			start = i
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}
