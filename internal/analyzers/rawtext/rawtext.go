// Package rawtext keeps every message the framework answers a request
// with inside the i18n catalog. A refusal is an i18n.Phrase: a catalog
// key whose last segment is the stable code clients branch on, English
// that i18n.Middleware's translator replaces per request. Text written
// any other way reaches a French caller in English and carries no code.
//
// The bug class: before the catalog, 168 http.Error sites and four
// JSON helpers taking `msg string` wrote English literals, so the
// error contract (a translated message beside a stable code) held only
// where someone remembered it. Each arm below is one spelling that
// sweep had to convert.
//
// Arms, all in the framework's own packages (core, framework, battery,
// core-ui under the module path):
//   - httperror: a call to net/http.Error. Write through
//     i18n.HTTPError (a Phrase) or i18n.HTTPErrorFor (an error).
//   - define: an i18n.Define call inside a function body (a func or a
//     func literal), or whose key or English is not a constant. The
//     catalog is the set of package-level definitions; a key minted at
//     request time cannot be translated ahead of time or listed.
//   - handlermsg: handler.Errorf, or a handler.Error literal setting
//     Message. Message is the app-author escape hatch (text sent as
//     written); framework code sets Reason.
//   - msgparam: a function or func literal taking an http.ResponseWriter
//     and a string parameter named msg/message/errMsg/errMessage/
//     errorMessage. A response helper takes a Phrase (plus params) so
//     every caller is handed the catalog by the type checker.
//   - envelope: a map literal whose "error" or "message" entry is prose
//     (a constant string holding a space) or an error's .Error() text.
//     A constant without a space is a code ("E_DENIED") and stays quiet.
//   - rawbody: a constant JSON body written straight to an
//     http.ResponseWriter (fmt.Fprint/Fprintf/Fprintln, io.WriteString,
//     w.Write([]byte(...))) whose "error", "message", "title" or
//     "detail" value is prose. It is the envelope arm for a body built
//     as text rather than a map.
//
// Silent postures, deliberately:
//   - _test.go files, and every package outside the four roots
//     (examples, cmd, kiln, internal): app code may write what it likes;
//   - a site that is the shape on purpose carries
//     //gofastr:allow(rawtext) <why>: i18n.HTTPError's own http.Error,
//     an agent-facing control API whose msg is a documented English
//     detail beside its own code.
package rawtext

import (
	"go/ast"
	"go/constant"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "rawtext",
	Doc:  "forbids framework responses written outside the i18n catalog: http.Error, i18n.Define off package level or with non-constant text, handler.Errorf/Message, response helpers taking a msg string, and prose or err.Error() in error envelopes and raw JSON bodies",
	Run:  run,
}

const module = "github.com/DonaldMurillo/gofastr/"

// inScope reports whether path is one of the framework's own packages.
func inScope(path string) bool {
	rest, ok := strings.CutPrefix(path, module)
	if !ok {
		return false
	}
	root, _, _ := strings.Cut(rest, "/")
	switch root {
	case "core", "framework", "battery", "core-ui":
		return true
	}
	return false
}

var msgParams = map[string]bool{
	"msg": true, "message": true, "errMsg": true, "errMessage": true, "errorMessage": true,
}

func run(pass *analysis.Pass) (any, error) {
	if !inScope(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		bodies := funcBodies(f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				checkCall(pass, n, bodies)
			case *ast.CompositeLit:
				checkLiteral(pass, n)
			case *ast.FuncDecl:
				checkParams(pass, n.Type, n.Name.Name)
			case *ast.FuncLit:
				checkParams(pass, n.Type, "func literal")
			}
			return true
		})
	}
	return nil, nil
}

// funcBodies collects f's function bodies, literals included: an
// i18n.Define inside one runs at call time, not at package init.
func funcBodies(f *ast.File) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Body != nil {
				out = append(out, n.Body)
			}
		case *ast.FuncLit:
			out = append(out, n.Body)
		}
		return true
	})
	return out
}

func inBody(bodies []*ast.BlockStmt, n ast.Node) bool {
	for _, b := range bodies {
		if b.Pos() <= n.Pos() && n.End() <= b.End() {
			return true
		}
	}
	return false
}

// callee is the package path and name of a call's static function.
func callee(pass *analysis.Pass, call *ast.CallExpr) (pkg, name string) {
	var id *ast.Ident
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fun.Sel
	case *ast.Ident:
		id = fun
	default:
		return "", ""
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return "", ""
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return "", ""
	}
	return fn.Pkg().Path(), fn.Name()
}

func checkCall(pass *analysis.Pass, call *ast.CallExpr, bodies []*ast.BlockStmt) {
	pkg, name := callee(pass, call)
	switch {
	case pkg == "net/http" && name == "Error":
		pass.Reportf(call.Pos(), "http.Error writes untranslated text with no code: answer through i18n.HTTPError with a Phrase, or i18n.HTTPErrorFor with an error")
	case pkg == module+"core/i18n" && name == "Define":
		if inBody(bodies, call) {
			pass.Reportf(call.Pos(), "i18n.Define inside a function, not a package-level var: the catalog is the set of package-level definitions, so a phrase minted here cannot be translated ahead of time")
			return
		}
		for _, arg := range call.Args {
			if tv, ok := pass.TypesInfo.Types[arg]; !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				pass.Reportf(arg.Pos(), "i18n.Define takes a constant key and constant English: a computed one cannot be listed in the catalog")
			}
		}
	case pkg == module+"core/handler" && name == "Errorf":
		pass.Reportf(call.Pos(), "handler.Errorf sends text as written, with no code: framework code answers with handler.Fail and a Phrase")
	case pkg == "fmt" && (name == "Fprint" || name == "Fprintf" || name == "Fprintln"),
		pkg == "io" && name == "WriteString":
		if len(call.Args) > 1 && isResponseWriter(pass.TypesInfo.TypeOf(call.Args[0])) {
			checkBody(pass, call.Args[1:])
		}
	case pkg == "" && isWriterWrite(pass, call):
		checkBody(pass, call.Args)
	}
}

// proseField matches a JSON "error", "message", "title" or "detail" key
// whose string value holds a space: prose, not a code.
var proseField = regexp.MustCompile(`"(error|message|title|detail)"\s*:\s*"[^"]*\s[^"]*"`)

// checkBody reports a constant argument (or []byte of one) that is a
// JSON body with a prose message.
func checkBody(pass *analysis.Pass, args []ast.Expr) {
	for _, arg := range args {
		if conv, ok := arg.(*ast.CallExpr); ok && len(conv.Args) == 1 {
			if tv, ok := pass.TypesInfo.Types[conv.Fun]; ok && tv.IsType() {
				arg = conv.Args[0]
			}
		}
		tv := pass.TypesInfo.Types[arg]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			continue
		}
		if m := proseField.FindStringSubmatch(constant.StringVal(tv.Value)); m != nil {
			pass.Reportf(arg.Pos(), "a raw JSON body's %s is English prose with no code: write the body from a Phrase (reason.Text(ctx) beside reason.Code())", m[1])
		}
	}
}

// isWriterWrite reports whether call is Write on an http.ResponseWriter.
func isWriterWrite(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Write" || len(call.Args) != 1 {
		return false
	}
	return isResponseWriter(pass.TypesInfo.TypeOf(sel.X))
}

func checkLiteral(pass *analysis.Pass, lit *ast.CompositeLit) {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return
	}
	if named, ok := types.Unalias(t).(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == module+"core/handler" && obj.Name() == "Error" {
			for _, e := range lit.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Message" {
						pass.Reportf(kv.Pos(), "handler.Error.Message sends text as written, with no code: framework code sets Reason to a Phrase")
					}
				}
			}
		}
	}
	m, ok := t.Underlying().(*types.Map)
	if !ok {
		return
	}
	if b, ok := m.Key().Underlying().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
		return
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := pass.TypesInfo.Types[kv.Key]
		if key.Value == nil || key.Value.Kind() != constant.String {
			continue
		}
		if k := constant.StringVal(key.Value); k != "error" && k != "message" {
			continue
		}
		if v := pass.TypesInfo.Types[kv.Value]; v.Value != nil && v.Value.Kind() == constant.String {
			if strings.Contains(constant.StringVal(v.Value), " ") {
				pass.Reportf(kv.Value.Pos(), "an envelope's %s is English prose with no code: write it from a Phrase (reason.Text(ctx) beside reason.Code())", constant.StringVal(key.Value))
			}
			continue
		}
		if isErrorText(pass, kv.Value) {
			pass.Reportf(kv.Value.Pos(), "an envelope's %s is an error's raw text: answer with the error's Phrase (i18n.ErrorText), never err.Error()", constant.StringVal(key.Value))
		}
	}
}

// isErrorText reports whether e is a call of Error() on an error value.
func isErrorText(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	return t != nil && types.Implements(t, errorType)
}

var errorType = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

func checkParams(pass *analysis.Pass, ft *ast.FuncType, name string) {
	writer := false
	var msg *ast.Ident
	for _, field := range ft.Params.List {
		t := pass.TypesInfo.TypeOf(field.Type)
		if t == nil {
			continue
		}
		if isResponseWriter(t) {
			writer = true
		}
		if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.String {
			for _, n := range field.Names {
				if msgParams[n.Name] && msg == nil {
					msg = n
				}
			}
		}
	}
	if writer && msg != nil {
		pass.Reportf(msg.Pos(), "%s writes a response from a %s string: take an i18n.Phrase (plus params) so every caller answers from the catalog", name, msg.Name)
	}
}

func isResponseWriter(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "ResponseWriter"
}
