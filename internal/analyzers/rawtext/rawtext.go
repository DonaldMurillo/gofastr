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
//     (a constant string holding a space) or an error's .Error() text,
//     or an HTTP error body struct (one with a json "status" field, the
//     RFC 9457 problem document shape) whose field tagged json "error",
//     "message", "title" or "detail" is prose. Protocol structs with no
//     status (JSON-RPC errors, the service control socket, the contracts
//     rule catalog) speak to agents and tools, and stay English.
//     A constant without a space is a code ("E_DENIED") and stays quiet.
//   - rawbody: a constant JSON body written straight to an
//     http.ResponseWriter (fmt.Fprint/Fprintf/Fprintln, io.WriteString,
//     w.Write([]byte(...))) whose "error", "message", "title" or
//     "detail" value is prose. It is the envelope arm for a body built
//     as text rather than a map.
//   - uitext: English written straight into markup a reader sees: a
//     constant passed to render.Text, a label attribute (aria-label,
//     aria-description, title, placeholder, alt) in a string-keyed map
//     literal such as html.Attrs, or a reader-facing string field
//     (Label, Title, Placeholder, AriaLabel, Alt, Heading, Description,
//     Text, Hint, Caption, Subtitle, Empty, or any name ending in Label,
//     Title, Text or Placeholder) of a component type from a UI package
//     (framework/ui, framework/headless, framework/entityui, core-ui).
//     Prose is a constant, or a concatenation with a constant operand,
//     holding two words or one capitalised word; a class, key, path,
//     symbol or all-capitals token ("GET, POST") stays quiet. Define the words as a Phrase and render phrase.Text(ctx),
//     or take them from the component's Strings (ui.StringsFor). The
//     runtime's JS twin is core-ui/check LintRuntimeProse.
//
// Silent postures, deliberately:
//   - _test.go files, and every package outside the four roots
//     (examples, cmd, kiln, internal): app code may write what it likes;
//   - uitext only: pages for developers rather than an app's readers:
//     the component gallery (framework/gallery) and a headless.Spec
//     literal's examples, which are sample data a developer browses, and
//     the SDK reference site (framework/sdkdocs), which documents the
//     API in English as the OpenAPI document does; and a
//     headless.Strings literal, the English defaults table that
//     ui.StringsFor's bridge test pins to i18nui's phrases;
//   - a site that is the shape on purpose carries
//     //gofastr:allow(rawtext) <why>: i18n.HTTPError's own http.Error,
//     an agent-facing control API whose msg is a documented English
//     detail beside its own code.
package rawtext

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "rawtext",
	Doc:  "forbids framework responses written outside the i18n catalog: http.Error, i18n.Define off package level or with non-constant text, handler.Errorf/Message, response helpers taking a msg string, prose or err.Error() in error envelopes and raw JSON bodies, and English written into markup (render.Text, label attributes, reader-facing component fields)",
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
		ui := uiReporter{pass: pass, specs: specLiterals(pass, f)}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				checkCall(pass, n, bodies, ui)
			case *ast.CompositeLit:
				checkLiteral(pass, n, ui)
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

func checkCall(pass *analysis.Pass, call *ast.CallExpr, bodies []*ast.BlockStmt, ui uiReporter) {
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
	case pkg == module+"core/render" && name == "Text" && len(call.Args) == 1:
		if s, ok := uiProse(pass, call.Args[0]); ok {
			ui.reportf(call.Args[0], "render.Text(%q) writes English into the page: define it as an i18n.Phrase at package level and render phrase.Text(ctx), or take it from the component's Strings", s)
		}
	}
}

// uiPackage reports whether path declares components: types whose
// string fields are rendered into markup.
func uiPackage(path string) bool {
	rest, ok := strings.CutPrefix(path, module)
	if !ok {
		return false
	}
	return rest == "framework/ui" || strings.HasPrefix(rest, "framework/ui/") ||
		rest == "framework/headless" || rest == "framework/entityui" ||
		strings.HasPrefix(rest, "core-ui/")
}

// docPackages draw pages for developers, not for an app's readers: the
// component gallery's demo content, and the SDK reference site, which
// documents the API in English the way the OpenAPI document does.
var docPackages = map[string]bool{
	"framework/gallery": true,
	"framework/sdkdocs": true,
}

// uiReporter reports uitext findings outside demo content: sample data
// a developer browses (the component gallery's package, a headless.Spec
// literal), not the framework's words.
type uiReporter struct {
	pass  *analysis.Pass
	specs []*ast.CompositeLit
}

func (u uiReporter) reportf(n ast.Node, format string, args ...any) {
	if docPackages[strings.TrimPrefix(u.pass.Pkg.Path(), module)] {
		return
	}
	for _, s := range u.specs {
		if s.Pos() <= n.Pos() && n.End() <= s.End() {
			return
		}
	}
	u.pass.Reportf(n.Pos(), format, args...)
}

// specLiterals collects f's headless.Spec and headless.Strings composite
// literals: demo content and the English defaults table.
func specLiterals(pass *analysis.Pass, f *ast.File) []*ast.CompositeLit {
	var out []*ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if named, ok := types.Unalias(pass.TypesInfo.TypeOf(lit)).(*types.Named); ok {
			obj := named.Obj()
			if obj.Pkg() != nil && obj.Pkg().Path() == module+"framework/headless" && (obj.Name() == "Spec" || obj.Name() == "Strings") {
				out = append(out, lit)
				return false
			}
		}
		return true
	})
	return out
}

// labelAttrs are the attributes whose value a reader hears or sees.
var labelAttrs = map[string]bool{
	"aria-label": true, "aria-description": true, "aria-roledescription": true,
	"title": true, "placeholder": true, "alt": true,
}

// uiFields are the reader-facing string fields of a framework type.
var uiFields = map[string]bool{
	"Label": true, "Title": true, "Placeholder": true, "AriaLabel": true, "Alt": true,
	"Heading": true, "Description": true, "Text": true, "Hint": true, "Caption": true,
	"Subtitle": true, "Empty": true, "EmptyText": true, "EmptyTitle": true,
}

// uiField reports whether a UI config field named name holds words a
// reader sees: one of uiFields, or any field ending in Label, Title,
// Text or Placeholder (SubmitLabel, TriggerLabel, EmptyTitle, AltText).
func uiField(name string) bool {
	if uiFields[name] {
		return true
	}
	for _, suffix := range []string{"Label", "Title", "Text", "Placeholder"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// reWords is two words; reCapWord one capitalised word.
var (
	reWords   = regexp.MustCompile(`\pL{2,}[,.:;!?)]*\s+\S*\pL{2,}`)
	reCapWord = regexp.MustCompile(`^\p{Lu}\p{Ll}{2,}[.!?…]?$`)
)

// uiProse reports e's constant text when it reads as words for a person.
// A concatenation is prose when one of its constant operands is: "Method
// " + m + " is not allowed" spells English around a value.
func uiProse(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return "", false
	}
	if bin, isBin := ast.Unparen(e).(*ast.BinaryExpr); isBin && tv.Value == nil && bin.Op == token.ADD {
		if s, ok := uiProse(pass, bin.X); ok {
			return s, true
		}
		return uiProse(pass, bin.Y)
	}
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	s := strings.TrimSpace(constant.StringVal(tv.Value))
	if strings.ContainsAny(s, "<>{}=") {
		return "", false // markup, a template or CSS, not a label
	}
	if strings.ToUpper(s) == s {
		return "", false // "GET, POST": tokens, not words
	}
	return s, reWords.MatchString(s) || reCapWord.MatchString(s)
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

func checkLiteral(pass *analysis.Pass, lit *ast.CompositeLit, ui uiReporter) {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return
	}
	if named, ok := types.Unalias(t).(*types.Named); ok {
		obj := named.Obj()
		if st, ok := named.Underlying().(*types.Struct); ok && obj.Pkg() != nil && uiPackage(obj.Pkg().Path()) {
			checkUIFields(pass, lit, st, ui)
		}
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
	if st, ok := t.Underlying().(*types.Struct); ok {
		checkEnvelopeFields(pass, lit, st)
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
		k := constant.StringVal(key.Value)
		if labelAttrs[k] {
			if s, ok := uiProse(pass, kv.Value); ok {
				ui.reportf(kv.Value, "%s %q is English written into the page: define it as an i18n.Phrase at package level and set phrase.Text(ctx), or take it from the component's Strings", k, s)
			}
			continue
		}
		if k != "error" && k != "message" {
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

// checkEnvelopeFields reports a field tagged json "error", "message",
// "title" or "detail" set to constant prose, on a struct that carries an
// HTTP status beside it.
func checkEnvelopeFields(pass *analysis.Pass, lit *ast.CompositeLit, st *types.Struct) {
	if !hasJSONField(st, "status") {
		return
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		for i := range st.NumFields() {
			if st.Field(i).Name() != id.Name {
				continue
			}
			name, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("json"), ",")
			if name != "error" && name != "message" && name != "title" && name != "detail" {
				break
			}
			if v := pass.TypesInfo.Types[kv.Value]; v.Value != nil && v.Value.Kind() == constant.String &&
				strings.Contains(constant.StringVal(v.Value), " ") {
				pass.Reportf(kv.Value.Pos(), "a body's %s is English prose with no code: write it from a Phrase (reason.Text(ctx) beside reason.Code())", name)
			}
			break
		}
	}
}

// hasJSONField reports whether st has a field whose json name is name.
func hasJSONField(st *types.Struct, name string) bool {
	for i := range st.NumFields() {
		if n, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("json"), ","); n == name {
			return true
		}
	}
	return false
}

// checkUIFields reports a reader-facing string field of a framework
// type set to English.
func checkUIFields(pass *analysis.Pass, lit *ast.CompositeLit, st *types.Struct, ui uiReporter) {
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok || !uiField(id.Name) {
			continue
		}
		for i := range st.NumFields() {
			f := st.Field(i)
			if f.Name() != id.Name {
				continue
			}
			if b, ok := f.Type().Underlying().(*types.Basic); !ok || b.Kind() != types.String {
				break
			}
			if s, ok := uiProse(pass, kv.Value); ok {
				ui.reportf(kv.Value, "%s: %q is English written into the page: define it as an i18n.Phrase at package level and set phrase.Text(ctx), or take it from the component's Strings", id.Name, s)
			}
			break
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
