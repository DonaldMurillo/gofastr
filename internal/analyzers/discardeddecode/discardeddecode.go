// Package discardeddecode catches a request-shaped parse whose error
// is thrown away: `_ = json.NewDecoder(r.Body).Decode(&body)`,
// `_ = json.Unmarshal(b, &v)`, `_ = r.ParseForm()`, and the bare
// statement spelling that drops the result on the floor.
//
// The bug class: the decode either read the caller's arguments or it
// did not, and discarding the error makes both cases look identical —
// the handler acks an operation whose arguments are the zero value.
// Probes TestPanelSendRedRejectsMalformed,
// TestPanelApproveRedRejectsMalformed, TestPanelRejectRedRejects
// Malformed (kiln/chat/panel.go serveSend/serveApprove/serveReject,
// 2026-09-02 round) and TestKilnDispatchRedRejectsMalformed
// (kiln/chat/server.go serveToolDispatch journaling a KindToolCall
// envelope from a body whose Unmarshal error was discarded) pinned
// the kiln family; examples/site/main.go servePaletteSearch discards
// r.ParseForm the same way. All still open. The checked control is
// server.go serveChatMessage: `if err := Decode(&args); err != nil` →
// 400.
//
// The family is the single-error-result parse set: json/xml/yaml
// Unmarshal, Decode on a json/xml Decoder, and ParseForm /
// ParseMultipartForm on an *http.Request — reported when the only
// result is blanked or dropped as a bare expression statement. The
// repo's own decoders belong to it too (core/handler UnmarshalStrict
// and DecodeStrict, internal/jsonexact Unmarshal and Decode), as does
// Decode on a yaml Decoder: until the 2026-11-02 guard bash the rule
// knew only the stdlib spellings, so the decoders every inbound seam
// is told to use could drop their error unseen.
//
// Division of labor with discardederr: that rule polices
// MULTI-VALUE assignments that drop an error while keeping the values
// around it (the refusal back-channel beside usable results) and is
// deliberately silent on `_ = f()`. This rule is the 1-result parse
// family, where `_ = f()` and the bare statement ARE the bug.
//
// Silent postures, deliberately:
//   - the error is assigned and checked, in an if-init or compared
//     against nil (`if json.Unmarshal(...) == nil`): the checked
//     spelling, by construction — only blanked or dropped results
//     report;
//   - a map-index source (`_ = json.Unmarshal(m["kty"], &kty)`):
//     probing one possibly-absent field of an envelope that was
//     itself decoded with its error checked; absence and malformed
//     collapse to the same zero, and the zero is vetted afterwards
//     (core/webbotauth jwks.go parseJWK). Decode/ParseForm take no
//     source operand and have no such spelling;
//   - _test.go files.
//
// The encode arm (2026-11 bug bash): json.Marshal / json.MarshalIndent
// with the error blanked (`b, _ := json.Marshal(v)`) and Encode on a
// json Encoder with its error blanked or dropped as a bare statement,
// when the encoded value is a JSON-RPC envelope (a struct with a field
// tagged json:"jsonrpc") that can actually fail to encode: it
// holds, at any depth through exported fields, slices, arrays, maps and
// pointers, an interface (any, map[string]any), a json.RawMessage, a
// float (NaN and ±Inf are refused), a chan, a func or a complex — and,
// since the 2026-11-03 round, a map whose key is none of a string, an
// integer and a TextMarshaler (map[float64]string is always refused,
// "unsupported key type"). Bug
// 3516c4d9b: core/mcp's transports encoded a dispatched Response (its
// Result is `any`) with the error dropped, so a NaN in a content block's
// _meta answered HTTP with an empty 200 body and stdio with no line at
// all; the client waited on a reply that never came. The fix spelling is
// core/mcp encodeResponse: check the error and answer an internal error
// instead. A composite literal is judged by what it sets: the fields a
// struct literal leaves out are zero and encode, and a map or slice
// literal by its elements. Scoped to envelopes because every encode that
// can fail measured 84 sites over ./... (57 with literals read by their
// values), mostly HTTP handlers acking with a map or a CRUD page of
// rows; a protocol peer waiting on its reply is where an empty body is
// a hang (protocolEnvelope carries the counts). Quiet: a non-envelope
// value, a value of only strings, ints, bools, byte slices and
// structs of them (the marshal cannot fail; only the write can, and a
// lost client is not news), a type that implements json.Marshaler or
// encoding.TextMarshaler (its own contract, time.Time among them),
// fields tagged json:"-" and unexported fields, and the checked
// spellings.
//
// One posture the bug narrative names that this rule deliberately
// does NOT take: "best-effort decodes into a value that is only
// logged or rendered as text". Syntax cannot tell a render-only sink
// from a march-on sink (kiln's journalled args are rendered too, and
// that site is the pinned bug), so no such silence exists; the
// whole-repo oracle output is the honest record of every discard.
package discardeddecode

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "discardeddecode",
	Doc:  "forbids discarding the error from a request-shaped parse (json/xml/yaml Unmarshal, Decoder.Decode, ParseForm), or from a json encode of a value that can fail to encode; a malformed body must be refused, not acked with zero-value arguments, and an unencodable reply answered, not sent empty",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.AssignStmt:
				if len(st.Rhs) == 1 && len(st.Lhs) == 2 {
					if id, ok := st.Lhs[1].(*ast.Ident); ok && id.Name == "_" {
						if call, ok := st.Rhs[0].(*ast.CallExpr); ok {
							if label, arg, ok := encodeCall(pass, call); ok && protocolEnvelope(pass, arg) {
								reportEncode(pass, call, label, pass.TypesInfo.TypeOf(arg))
							}
						}
					}
				}
				for i, rhs := range st.Rhs {
					if i >= len(st.Lhs) {
						break
					}
					id, ok := st.Lhs[i].(*ast.Ident)
					if !ok || id.Name != "_" {
						continue
					}
					call, ok := rhs.(*ast.CallExpr)
					if !ok {
						continue
					}
					if label, ok := familyCall(pass, call); ok {
						report(pass, call, label)
					}
					if label, arg, ok := encoderCall(pass, call); ok && protocolEnvelope(pass, arg) {
						reportEncode(pass, call, label, pass.TypesInfo.TypeOf(arg))
					}
				}

			case *ast.ExprStmt:
				call, ok := st.X.(*ast.CallExpr)
				if !ok {
					return true
				}
				if label, ok := familyCall(pass, call); ok {
					report(pass, call, label)
				}
				if label, arg, ok := encoderCall(pass, call); ok && protocolEnvelope(pass, arg) {
					reportEncode(pass, call, label, pass.TypesInfo.TypeOf(arg))
				}
			}
			return true
		})
	}
	return nil, nil
}

func report(pass *analysis.Pass, call *ast.CallExpr, label string) {
	pass.Reportf(call.Pos(),
		"%s error discarded: the parse failure marches on as zero-value data; check the error and refuse the input",
		label)
}

// familyCall reports whether call is in the 1-result parse family and
// its label: json/xml/yaml Unmarshal, Decode on a json/xml Decoder,
// ParseForm/ParseMultipartForm on an *http.Request.
func familyCall(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	// Package-qualified calls: json.Unmarshal, yaml.Unmarshal, ...
	if id := identOf(sel.X); id != nil {
		pkgName, _ := pass.TypesInfo.ObjectOf(id).(*types.PkgName)
		if pkgName != nil && sel.Sel.Name == "Unmarshal" && len(call.Args) > 0 {
			if _, isProbe := call.Args[0].(*ast.IndexExpr); isProbe {
				return "", false // map-index optional-field probe
			}
			switch path := pkgName.Imported().Path(); path {
			case "encoding/json":
				return "json.Unmarshal", true
			case "encoding/xml":
				return "xml.Unmarshal", true
			default:
				if isYAML(path) {
					return "yaml.Unmarshal", true
				}
			}
		}
		if pkgName != nil {
			// The repo's own decoders, the spellings CLAUDE.md steers
			// every inbound seam to: discarding their error is the same
			// bug, and the zero value marches on all the same.
			switch pkgName.Imported().Path() + "." + sel.Sel.Name {
			case handlerPath + ".UnmarshalStrict", handlerPath + ".DecodeStrict",
				jsonexactPath + ".Unmarshal", jsonexactPath + ".Decode":
				if len(call.Args) > 0 {
					if _, isProbe := call.Args[0].(*ast.IndexExpr); isProbe {
						return "", false // map-index optional-field probe
					}
				}
				return pkgName.Name() + "." + sel.Sel.Name, true
			}
			return "", false
		}
	}
	// Method calls: Decode on *json.Decoder / *xml.Decoder, and
	// ParseForm / ParseMultipartForm on *http.Request.
	recv := receiverType(pass, sel)
	switch {
	case recv == "encoding/json.Decoder" && sel.Sel.Name == "Decode":
		return "Decode", true
	case recv == "encoding/xml.Decoder" && sel.Sel.Name == "Decode":
		return "Decode", true
	case strings.HasSuffix(recv, ".Decoder") && isYAML(strings.TrimSuffix(recv, ".Decoder")) && sel.Sel.Name == "Decode":
		return "Decode", true
	case recv == "net/http.Request" && (sel.Sel.Name == "ParseForm" || sel.Sel.Name == "ParseMultipartForm"):
		return sel.Sel.Name, true
	}
	return "", false
}

// identOf returns e as an identifier when it is one.
func identOf(e ast.Expr) *ast.Ident {
	id, _ := e.(*ast.Ident)
	return id
}

// receiverType renders the receiver's named type as "importpath.Type"
// (pointer and alias resolved), or "" when it is not a named type.
func receiverType(pass *analysis.Pass, sel *ast.SelectorExpr) string {
	t := pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		if selection := pass.TypesInfo.Selections[sel]; selection != nil {
			t = selection.Recv()
		}
	}
	if t == nil {
		return ""
	}
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return ""
	}
	// The universe error's Named has no package (err.Error() reaches
	// here through familyCall): no path, no decoder family.
	if named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}

// encodableMapKey reports whether encoding/json accepts a key of type
// t: a string or integer kind, or a MarshalText. Anything else — a
// float, a bool, a struct without MarshalText, an interface — is
// refused at encode time with "unsupported key type", an error a
// dropped Encode swallows.
func encodableMapKey(t types.Type) bool {
	for _, tt := range []types.Type{t, types.NewPointer(t)} {
		if obj, _, _ := types.LookupFieldOrMethod(tt, true, nil, "MarshalText"); obj != nil {
			if _, ok := obj.(*types.Func); ok {
				return true
			}
		}
	}
	if b, ok := t.Underlying().(*types.Basic); ok {
		return b.Info()&(types.IsString|types.IsInteger) != 0
	}
	return false
}

// isYAML reports whether an import path is one of the yaml packages
// whose Unmarshal is in the family.
// The repo's strict and lossless decoders.
const (
	handlerPath   = "github.com/DonaldMurillo/gofastr/core/handler"
	jsonexactPath = "github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

func isYAML(path string) bool {
	return path == "gopkg.in/yaml.v2" || path == "gopkg.in/yaml.v3" || path == "sigs.k8s.io/yaml"
}

func reportEncode(pass *analysis.Pass, call *ast.CallExpr, label string, t types.Type) {
	pass.Reportf(call.Pos(),
		"%s error discarded on a %s, which can fail to encode (an interface, float, RawMessage, chan or func inside): the reader gets an empty body or no line at all; check the error and answer a failure",
		label, types.TypeString(t, types.RelativeTo(pass.Pkg)))
}

// encodeCall matches json.Marshal(v) / json.MarshalIndent(v, ...) and
// returns the encoded operand.
func encodeCall(pass *analysis.Pass, call *ast.CallExpr) (string, ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", nil, false
	}
	id := identOf(sel.X)
	if id == nil {
		return "", nil, false
	}
	pkgName, _ := pass.TypesInfo.ObjectOf(id).(*types.PkgName)
	if pkgName == nil || pkgName.Imported().Path() != "encoding/json" {
		return "", nil, false
	}
	switch sel.Sel.Name {
	case "Marshal", "MarshalIndent":
		return "json." + sel.Sel.Name, call.Args[0], true
	}
	return "", nil, false
}

// encoderCall matches Encode on a *json.Encoder and returns the encoded
// operand.
func encoderCall(pass *analysis.Pass, call *ast.CallExpr) (string, ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Encode" || len(call.Args) != 1 {
		return "", nil, false
	}
	if receiverType(pass, sel) != "encoding/json.Encoder" {
		return "", nil, false
	}
	return "Encode", call.Args[0], true
}

// protocolEnvelope reports whether e is a JSON-RPC envelope (a struct,
// or a pointer to one, with a field tagged json:"jsonrpc") whose encode
// can fail. The arm is scoped to envelopes: measured over ./... on
// 2026-11-02, every encode of a value that can fail found 84 sites,
// and 57 once literal maps were read by the values they hold, mostly
// HTTP handlers acking with a map or a CRUD page of rows. An envelope is
// the case where the reader is a protocol peer that waits on the
// reply: an empty body or a missing line is a hang, not a blank page.
func protocolEnvelope(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range st.NumFields() {
		if jsonName(st.Tag(i)) == "jsonrpc" {
			return canFailExpr(pass, e)
		}
	}
	return false
}

// canFailExpr reports whether encoding e can fail. A composite literal
// is read element by element, so map[string]any{"ok": true, "id": id}
// is judged by the values it actually holds, not by the `any` its type
// admits: an any-valued literal of strings and ints cannot fail.
func canFailExpr(pass *analysis.Pass, e ast.Expr) bool {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = ast.Unparen(u.X)
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return canFailEncode(pass.TypesInfo.TypeOf(e))
	}
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return false
	}
	if isRawMessage(t) || marshalsItself(t) {
		return canFailEncode(t)
	}
	switch u := t.Underlying().(type) {
	case *types.Map:
		// An empty map encodes as {} whatever its key type; every
		// entry present is judged by its key, which json may refuse.
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if kt := pass.TypesInfo.TypeOf(kv.Key); kt == nil || !encodableMapKey(kt) {
				return true
			}
		}
	case *types.Slice, *types.Array:
	case *types.Struct:
		// Fields the literal leaves out are zero: a nil interface or
		// RawMessage encodes as null and a zero float as 0, so only the
		// fields it sets can fail.
		for i, el := range lit.Elts {
			field := -1
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					for j := range u.NumFields() {
						if u.Field(j).Name() == id.Name {
							field = j
						}
					}
				}
			} else if i < u.NumFields() {
				field = i
			}
			if field >= 0 {
				f := u.Field(field)
				if jsonName(u.Tag(field)) == "-" || (!f.Exported() && !f.Embedded()) {
					continue
				}
			}
			if canFailExpr(pass, el) {
				return true
			}
		}
		return false
	default:
		return canFailEncode(t)
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			el = kv.Value
		}
		if canFailExpr(pass, el) {
			return true
		}
	}
	return false
}

// canFailEncode reports whether encoding/json can refuse a value of
// static type t: it holds an interface, a json.RawMessage, a float, a
// chan, a func or a complex where the encoder reaches.
func canFailEncode(t types.Type) bool {
	return failWalk(t, map[types.Type]bool{})
}

func failWalk(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if isRawMessage(t) {
		return true
	}
	t = types.Unalias(t)
	if _, isIface := t.Underlying().(*types.Interface); !isIface && marshalsItself(t) {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Basic:
		return u.Info()&(types.IsFloat|types.IsComplex) != 0 || u.Kind() == types.UnsafePointer
	case *types.Chan, *types.Signature:
		return true
	case *types.Pointer:
		return failWalk(u.Elem(), seen)
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return false // base64
		}
		return failWalk(u.Elem(), seen)
	case *types.Array:
		return failWalk(u.Elem(), seen)
	case *types.Map:
		// json.Marshal refuses a map whose key is neither a string,
		// an integer, nor a TextMarshaler: the key itself can fail.
		if !encodableMapKey(u.Key()) {
			return true
		}
		return failWalk(u.Elem(), seen)
	case *types.Struct:
		for i := range u.NumFields() {
			f := u.Field(i)
			if jsonName(u.Tag(i)) == "-" || (!f.Exported() && !f.Embedded()) {
				continue
			}
			if failWalk(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// isRawMessage matches json.RawMessage, a defined type or (under the
// jsonv2 experiment) an alias of jsontext.Value.
func isRawMessage(t types.Type) bool {
	if a, ok := t.(*types.Alias); ok && a.Obj().Pkg() != nil && a.Obj().Pkg().Path() == "encoding/json" && a.Obj().Name() == "RawMessage" {
		return true
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	p, name := n.Obj().Pkg().Path(), n.Obj().Name()
	return p == "encoding/json" && name == "RawMessage" || p == "encoding/json/jsontext" && name == "Value"
}

// marshalsItself reports whether t (or *t) has a MarshalJSON or
// MarshalText method: its encoding is its own contract.
func marshalsItself(t types.Type) bool {
	for _, name := range []string{"MarshalJSON", "MarshalText"} {
		for _, tt := range []types.Type{t, types.NewPointer(t)} {
			if obj, _, _ := types.LookupFieldOrMethod(tt, true, nil, name); obj != nil {
				if _, ok := obj.(*types.Func); ok {
					return true
				}
			}
		}
	}
	return false
}

// jsonName returns the name part of a struct field's json tag.
func jsonName(tag string) string {
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
	return name
}
