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
// result is blanked or dropped as a bare expression statement.
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
// One posture the bug narrative names that this rule deliberately
// does NOT take: "best-effort decodes into a value that is only
// logged or rendered as text". Syntax cannot tell a render-only sink
// from a march-on sink (kiln's journalled args are rendered too, and
// that site is the pinned bug), so no such silence exists; the
// whole-repo oracle output is the honest record of every discard.
package discardeddecode

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "discardeddecode",
	Doc:  "forbids discarding the error from a request-shaped parse (json/xml/yaml Unmarshal, Decoder.Decode, ParseForm); a malformed body must be refused, not acked with zero-value arguments",
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
				}

			case *ast.ExprStmt:
				call, ok := st.X.(*ast.CallExpr)
				if !ok {
					return true
				}
				if label, ok := familyCall(pass, call); ok {
					report(pass, call, label)
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
	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}

// isYAML reports whether an import path is one of the yaml packages
// whose Unmarshal is in the family.
func isYAML(path string) bool {
	return path == "gopkg.in/yaml.v2" || path == "gopkg.in/yaml.v3" || path == "sigs.k8s.io/yaml"
}
