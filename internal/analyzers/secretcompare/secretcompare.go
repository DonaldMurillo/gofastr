// Package secretcompare catches a client-supplied credential compared
// with == or != instead of a constant-time primitive.
//
// The bug class: Go's string compare short-circuits on the first
// mismatched byte, so comparing a secret against a guess leaks how many
// leading bytes were right through response timing. Probe
// TestMintCodeRedConstantTimeCompare (2026-09-03 adversarial pass round
// 5) pinned it in framework/experimental/harness/control/auth
// issuance.go Issuer.Confirm: the user-supplied 6-digit mint code was
// compared with `code != p.code` while every other client-supplied
// credential compare in the auth family is constant-time (battery/setup
// token.go tokenEqual and battery/auth twofa.go TOTP via
// subtle.ConstantTimeCompare; oauth state, webhook signatures, and the
// harness bearer Decoder via hmac.Equal). Confirm now uses
// subtle.ConstantTimeCompare.
//
// The rule reports an ==/!= between two runtime string or []byte
// values where a credential-shaped operand faces a guessable side:
// another credential-named value, or an input-fetching call (os.Getenv,
// header/query/form/cookie reads). An operand is credential-shaped
// when its identifier (variable, field, or parameter) carries one of
// the credential words code, token, secret, otp, nonce, pin, password,
// passwd, apikey, signature, sig, mac, bearer, csrf as a WHOLE word
// under camelCase / underscore splitting (apiKey counts as api+key;
// zipcode and encoding are single words that contain the letters, not
// the token, and stay quiet).
//
// The compare has more spellings than ==/!=, and each short-circuits
// the same way: bytes.Equal on the pair (bytes.Equal on a MAC is the
// textbook timing bug), bytes.Compare or strings.Compare whose result is
// tested == 0 / != 0, a switch whose
// tag is compared against each case, and an operand behind parentheses
// or a string <-> []byte conversion (`string(sig) == want`). A type
// parameter whose every constraint term is a string or []byte counts as
// one. The 2026-11-02 guard bash found the rule blind to all of them.
//
// Silent postures, deliberately:
//   - a literal, named constant, or nil on either side: an empty-check
//     (`code == ""`), a sentinel, a typed enum constant, and `!= nil`
//     are not compares of two secrets;
//   - lengths (`len(code) != len(p.code)`) and any other computed
//     operand: only identifiers and field selectors carry the
//     credential name;
//   - non-string operands: enum ints and structs named Token have no
//     byte-at-a-time short-circuit to leak;
//   - a credential against a value with no guessable side: another
//     server-held copy read into a plain local (oidc's `tokenSub !=
//     uiSub`, the oauth store's RefreshToken guard) or any computed
//     non-input value. The compare must face either another
//     credential-named operand or an input-fetching call (os.Getenv,
//     header/query/form/cookie reads) for the timing to carry
//     attacker-steerable information;
//   - hash- and digest-named operands (content digests, not
//     credentials; see credentialWords below);
//   - a bytes.Compare / strings.Compare used to ORDER, not to match: a
//     sort comparator returning it (battery/auth's nonce eviction sorts
//     by nonce), or a `< 0` test. Ordering two values decides no match
//     an attacker can probe for; only a result tested against zero is
//     an equality check;
//   - the fix posture itself: subtle.ConstantTimeCompare(x, y) == 1
//     compares a call result against a literal and never names a
//     credential operand;
//   - _test.go files.
package secretcompare

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "secretcompare",
	Doc:  "forbids comparing a credential-named string with == or != (short-circuit compare leaks match timing); use subtle.ConstantTimeCompare or hmac.Equal",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				if v.Op == token.EQL || v.Op == token.NEQ {
					// bytes.Compare / strings.Compare tested against
					// zero is == spelled as a call. A Compare whose
					// result orders (a sort comparator returning it, a
					// `< 0`) decides no match and stays quiet.
					if call, name := compareAgainstZero(pass, v.X, v.Y); call != nil {
						check(pass, call.Args[0], call.Args[1], v.Pos(), name+"(...) == 0")
						return true
					}
					check(pass, v.X, v.Y, v.Pos(), "==/!=")
				}
			case *ast.CallExpr:
				// bytes.Equal is == spelled as a call: the same
				// byte-at-a-time short circuit.
				if pathflow.QualifiedFunc(pass, v.Fun) == "bytes.Equal" && len(v.Args) == 2 {
					check(pass, v.Args[0], v.Args[1], v.Pos(), "bytes.Equal")
				}
			case *ast.SwitchStmt:
				// `switch token { case want: }` compares the tag with ==
				// against every case expression.
				if v.Tag == nil {
					return true
				}
				for _, st := range v.Body.List {
					cc, ok := st.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, e := range cc.List {
						check(pass, v.Tag, e, e.Pos(), "a switch case")
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

// compareAgainstZero returns the bytes.Compare / strings.Compare call
// on one side of an ==/!= whose other side is the constant 0, and the
// call's name; nil when the pair is anything else.
func compareAgainstZero(pass *analysis.Pass, x, y ast.Expr) (*ast.CallExpr, string) {
	for _, pair := range [2][2]ast.Expr{{x, y}, {y, x}} {
		call, ok := ast.Unparen(pair[0]).(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			continue
		}
		name := pathflow.QualifiedFunc(pass, call.Fun)
		if name != "bytes.Compare" && name != "strings.Compare" {
			continue
		}
		tv, ok := pass.TypesInfo.Types[pair[1]]
		if !ok || tv.Value == nil || tv.Value.String() != "0" {
			continue
		}
		return call, name
	}
	return nil, ""
}

// check reports a compare of x against y at pos when one side is
// credential-shaped and the other is guessable. how names the compare's
// spelling for the message.
func check(pass *analysis.Pass, x, y ast.Expr, pos token.Pos, how string) {
	x, y = unwrap(pass, x), unwrap(pass, y)
	if !stringish(pass, x) || !stringish(pass, y) {
		return
	}
	xName, xCred := operandCredential(x)
	yName, yCred := operandCredential(y)
	if !xCred && !yCred {
		return
	}
	// A literal, constant, or nil on either side is an empty-check /
	// sentinel / enum compare, not a compare of two secrets.
	if isConstOperand(pass, x) || isConstOperand(pass, y) {
		return
	}
	// A credential against a plain local of unknown provenance leaks
	// nothing an attacker can steer: both surviving shapes have a
	// guessable side, either another credential-named value (the
	// client's presentation) or an input-fetching call (env, header,
	// query, form).
	other := y
	if yCred {
		other = x
	}
	if !xCred || !yCred {
		if _, ok := operandName(other); ok || !inputFetch(pass, other) {
			return
		}
	}
	named := xName
	if named == "" {
		named = yName
	}
	pass.Reportf(pos,
		"%s compared with %s: a short-circuit compare on a credential leaks match status through timing; use subtle.ConstantTimeCompare or hmac.Equal",
		named, how)
}

// unwrap strips what does not change the bytes compared: parentheses
// and a string <-> []byte conversion (`string(sig) == want` compares
// sig's bytes). A conversion is a call whose callee is a type, which the
// type checker tells apart from a computed call.
func unwrap(pass *analysis.Pass, e ast.Expr) ast.Expr {
	for {
		switch v := e.(type) {
		case *ast.ParenExpr:
			e = v.X
			continue
		case *ast.CallExpr:
			if len(v.Args) == 1 {
				if tv, ok := pass.TypesInfo.Types[v.Fun]; ok && tv.IsType() && stringish(pass, v.Args[0]) {
					e = v.Args[0]
					continue
				}
			}
		}
		return e
	}
}

// operandName returns the identifier a comparison operand is named by:
// the variable for an identifier, the field for a selector (p.code is
// the field code). Computed operands (len(x), parts[2], calls) carry no
// credential name.
func operandName(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		if v.Name == "_" || v.Name == "nil" {
			return "", false
		}
		return v.Name, true
	case *ast.SelectorExpr:
		return v.Sel.Name, true
	}
	return "", false
}

// isConstOperand reports whether e is nil, a basic literal, a constant
// expression, or a reference to a named constant: the sentinel shapes
// the rule does not read as a compare of two secrets.
func isConstOperand(pass *analysis.Pass, e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		if v.Name == "nil" {
			return true
		}
		if tv, ok := pass.TypesInfo.Types[v]; ok && tv.Value != nil {
			return true
		}
		if obj := pass.TypesInfo.ObjectOf(v); obj != nil {
			if _, ok := obj.(*types.Const); ok {
				return true
			}
		}
		return false
	case *ast.SelectorExpr:
		if obj := pass.TypesInfo.ObjectOf(v.Sel); obj != nil {
			if _, ok := obj.(*types.Const); ok {
				return true
			}
		}
		if tv, ok := pass.TypesInfo.Types[v]; ok && tv.Value != nil {
			return true
		}
		return false
	case *ast.BasicLit:
		return true
	}
	if tv, ok := pass.TypesInfo.Types[e]; ok && tv.Value != nil {
		return true
	}
	return false
}

// stringish reports whether t is a string or a []byte: the types whose
// ==/!= compares byte-prefix short-circuit. Named string types share
// the underlying kind and count.
func stringish(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	return stringType(t)
}

// stringType reports whether t is a string or a []byte, or a type
// parameter every term of whose constraint is one (`T ~string`).
func stringType(t types.Type) bool {
	if tp, ok := t.(*types.TypeParam); ok {
		iface, ok := tp.Constraint().Underlying().(*types.Interface)
		if !ok || iface.NumEmbeddeds() == 0 {
			return false
		}
		for i := range iface.NumEmbeddeds() {
			switch et := iface.EmbeddedType(i).(type) {
			case *types.Union:
				for j := range et.Len() {
					if !stringType(et.Term(j).Type()) {
						return false
					}
				}
			default:
				if !stringType(et) {
					return false
				}
			}
		}
		return true
	}
	u := t.Underlying()
	switch v := u.(type) {
	case *types.Basic:
		return v.Info()&types.IsString != 0
	case *types.Slice:
		b, ok := u.(*types.Slice).Elem().Underlying().(*types.Basic)
		return ok && b.Kind() == types.Byte
	}
	return false
}

// credentialWords are the whole-word name tokens that mark an operand
// as a credential. apikey covers the flat lowercase spelling; the
// api+key pair below covers apiKey / APIKey. hash and digest are
// deliberately absent: the 2026-09-03 whole-repo run measured five
// hash-named compares and every one was a content digest (wasm asset
// identity, SDK schema drift, an ETag version param, a bcrypt
// list-bookkeeping match, a generator watch loop), zero credentials.
var credentialWords = map[string]bool{
	"code": true, "token": true, "secret": true, "otp": true,
	"nonce": true, "pin": true, "password": true, "passwd": true,
	"apikey": true, "signature": true, "sig": true, "mac": true,
	"bearer": true, "csrf": true,
}

// operandCredential returns the operand's name and whether it is
// credential-shaped.
func operandCredential(e ast.Expr) (string, bool) {
	name, ok := operandName(e)
	if !ok {
		return "", false
	}
	return name, credentialWord(name)
}

// inputFetch reports whether e is a call that reads attacker-steerable
// input: os.Getenv, or a Get/FormValue/PostFormValue/Cookie method on
// a receiver naming header/query/form/cookie storage.
func inputFetch(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if pathflow.QualifiedFunc(pass, call.Fun) == "os.Getenv" {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Get", "FormValue", "PostFormValue", "Cookie", "Cookies":
	default:
		return false
	}
	mentionsInput := false
	ast.Inspect(sel.X, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			l := strings.ToLower(id.Name)
			if strings.Contains(l, "header") || strings.Contains(l, "query") ||
				strings.Contains(l, "form") || strings.Contains(l, "cookie") {
				mentionsInput = true
				return false
			}
		}
		return !mentionsInput
	})
	return mentionsInput
}

// credentialWord reports whether the identifier name carries a
// credential word as a whole word: zipcode and encoding contain the
// letters "code" but are single unrelated words and stay quiet;
// confirmationCode, p.code, and apiKey carry it.
func credentialWord(name string) bool {
	words := astx.SplitWords(name)
	for i, w := range words {
		if credentialWords[strings.ToLower(w)] {
			return true
		}
		// apiKey / APIKey split as api+key.
		if strings.EqualFold(w, "api") && i+1 < len(words) && strings.EqualFold(words[i+1], "key") {
			return true
		}
	}
	return false
}
