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
// harness bearer Decoder via hmac.Equal). The bug is still open.
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
	"unicode"

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
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			if !stringish(pass, be.X) || !stringish(pass, be.Y) {
				return true
			}
			xName, xCred := operandCredential(be.X)
			yName, yCred := operandCredential(be.Y)
			if !xCred && !yCred {
				return true
			}
			// A literal, constant, or nil on either side is an
			// empty-check / sentinel / enum compare, not a compare of
			// two secrets.
			if isConstOperand(pass, be.X) || isConstOperand(pass, be.Y) {
				return true
			}
			// A credential against a plain local of unknown provenance
			// leaks nothing an attacker can steer: both surviving shapes
			// have a guessable side, either another credential-named
			// value (the client's presentation) or an input-fetching
			// call (env, header, query, form).
			other := be.Y
			if yCred {
				other = be.X
			}
			if !xCred || !yCred {
				if _, ok := operandName(other); ok || !inputFetch(pass, other) {
					return true
				}
			}
			named := xName
			if named == "" {
				named = yName
			}
			pass.Reportf(be.Pos(),
				"%s compared with ==/!=: a short-circuit compare on a credential leaks match status through timing; use subtle.ConstantTimeCompare or hmac.Equal",
				named)
			return true
		})
	}
	return nil, nil
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
	if qualifiedFunc(pass, call.Fun) == "os.Getenv" {
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

// credentialWord reports whether the identifier name carries a
// credential word as a whole word: zipcode and encoding contain the
// letters "code" but are single unrelated words and stay quiet;
// confirmationCode, p.code, and apiKey carry it.
func credentialWord(name string) bool {
	words := splitWords(name)
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

// splitWords splits an identifier into its camelCase / underscore
// words: confirmationCode -> [confirmation Code], api_key ->
// [api key], APIKey -> [API Key], zipcode -> [zipcode].
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
