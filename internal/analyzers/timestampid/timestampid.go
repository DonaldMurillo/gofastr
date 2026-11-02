// Package timestampid catches an identifier minted from wall-clock
// time: a time.Now().Unix()/.UnixMilli()/.UnixNano() value formatted
// into a string (fmt.Sprintf, strconv.FormatInt/FormatUint/Itoa, or +
// concatenation) and bound to a name ending in id, token, session,
// a credential-qualified key (apiKey, signingKey), nonce, secret, or
// capability.
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
// All of these are fixed: the ids now mint from kid.Hex (crypto/rand).
//
// The rule fires when the formatted wall-clock value is bound to a
// credential-shaped name: a variable or struct field whose last
// camelCase word is one of the seven suffixes above, or a return from
// a function so named. Every binding spelling counts — an assignment
// (multi-value included), a `var token = …` declaration, and a keyed
// struct-literal field (Session{ID: …}) — and so does every format:
// fmt.Sprint/Sprintln beside Sprintf, and the clock rendered as text
// with time.Now().Format / .String / .AppendFormat. Until the
// 2026-11-02 guard bash only a single-value assignment and the Unix*
// readings counted, and a make([]byte, 16) rand buffer was never sized
// (the builtin is an identifier, not a pkg.Func selector), so the
// 16-byte silence below held for arrays only.
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
//   - a bare or place-qualified key: a storage object's Key field, a
//     cacheKey, an objectKey. Date-bucketed keys
//     (Object{Key: "backups/" + time.Now().Format(...)}) name WHERE a
//     thing lives, and the store's own access control decides who may
//     read it; guessing the name grants nothing. Round 3's Format and
//     struct-literal arms fired on exactly these. A trailing "key"
//     counts only behind a credential word (apiKey, signingKey,
//     sessionKey, secretKey: credentialKeyWords below);
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

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
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
			if len(st.Lhs) != len(st.Rhs) {
				return true
			}
			for i, lhs := range st.Lhs {
				name, ok := bindingName(lhs)
				if !ok || !capabilityName(name) {
					continue
				}
				if minted(pass, st.Rhs[i], bound, bigRand, 0) {
					report(pass, st.Pos(), name)
				}
			}
		case *ast.ValueSpec:
			// var sessionToken = fmt.Sprintf(...): the declaration
			// spelling of the assignment above.
			if len(st.Names) != len(st.Values) {
				return true
			}
			for i, id := range st.Names {
				if id.Name == "_" || !capabilityName(id.Name) {
					continue
				}
				if minted(pass, st.Values[i], bound, bigRand, 0) {
					report(pass, st.Pos(), id.Name)
				}
			}
		case *ast.KeyValueExpr:
			// Session{ID: fmt.Sprintf(...)}: a keyed field of a struct
			// literal, the field-assignment spelling.
			key, ok := st.Key.(*ast.Ident)
			if !ok || !capabilityName(key.Name) {
				return true
			}
			if _, isField := pass.TypesInfo.ObjectOf(key).(*types.Var); !isField {
				return true // a map key or other non-field spelling
			}
			if minted(pass, st.Value, bound, bigRand, 0) {
				report(pass, st.Pos(), key.Name)
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
	words := astx.SplitWords(name)
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
	// "key" alone names a place as often as a credential: a storage
	// object's Key, a cacheKey, a date-bucketed objectKey. Only a key a
	// credential word qualifies (apiKey, signingKey, sessionKey) is one.
	if last == "key" {
		return len(words) >= 2 && credentialKeyWords[strings.ToLower(words[len(words)-2])]
	}
	return true
}

// credentialKeyWords qualify a trailing "key" as a credential.
var credentialKeyWords = map[string]bool{
	"api": true, "access": true, "secret": true, "private": true,
	"signing": true, "session": true, "auth": true, "encryption": true,
	"hmac": true, "master": true, "license": true, "invite": true,
	"share": true, "capability": true,
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
		q := pathflow.QualifiedFunc(pass, v.Fun)
		switch q {
		case "fmt.Sprintf", "fmt.Sprint", "fmt.Sprintln", "strconv.FormatInt", "strconv.FormatUint", "strconv.Itoa":
			for _, a := range v.Args {
				if mentionsWallClock(pass, a, bound, depth) {
					return true
				}
			}
		}
		// time.Now().Format(layout) / .String(): the clock itself
		// formatted as text is the same enumerable mint.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && clockText[sel.Sel.Name] && isNow(pass, sel.X, bound, depth) {
			return true
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
		switch {
		case sel.Sel.Name == "Unix", sel.Sel.Name == "UnixMilli", sel.Sel.Name == "UnixMicro", sel.Sel.Name == "UnixNano":
		case clockText[sel.Sel.Name]:
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

// isBuiltin reports whether fun names the predeclared builtin name.
func isBuiltin(pass *analysis.Pass, fun ast.Expr, name string) bool {
	id, ok := fun.(*ast.Ident)
	if !ok || id.Name != name {
		return false
	}
	_, ok = pass.TypesInfo.ObjectOf(id).(*types.Builtin)
	return ok
}

// clockText are the time.Time methods that render the clock reading as
// text: an id built from one carries the same zero unpredictable bits
// as a formatted UnixNano.
var clockText = map[string]bool{"Format": true, "String": true, "AppendFormat": true}

// isNow reports whether e is time.Now() itself or a local whose
// binding is time.Now(). A clock passed as a parameter is the
// injectable-clock posture and is not wall-clock here.
func isNow(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) bool {
	if call, ok := e.(*ast.CallExpr); ok && pathflow.QualifiedFunc(pass, call.Fun) == "time.Now" {
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
		if pathflow.QualifiedFunc(pass, call.Fun) != "crypto/rand.Read" {
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
		// make is a builtin, an identifier: QualifiedFunc reads only
		// pkg.Func selectors, so the old `QualifiedFunc(...) == "make"`
		// test never matched and a make([]byte, 16) buffer read as
		// unsized (2026-11-02 guard bash: the documented 16-byte
		// silence held only for arrays).
		if call, ok := b.(*ast.CallExpr); ok && isBuiltin(pass, call.Fun, "make") {
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
