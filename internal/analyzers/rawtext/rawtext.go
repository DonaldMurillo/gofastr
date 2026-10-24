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
// core-ui under the module path) and examples/meridian:
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
//     symbol or all-capitals token ("GET, POST") stays quiet. A local
//     variable (a parameter included) assigned such a constant anywhere
//     in its function is prose at every sink it reaches: StepRail's
//     `if aria == "" { aria = "Page steps" }` default shipped English
//     one assignment away from its aria-label. A
//     package's own helper whose string parameter reaches one of those
//     sinks, directly or through another such helper, makes that
//     argument a sink at its call sites (textparam). Define the words
//     as a Phrase and render phrase.Text(ctx), or take them from the
//     component's Strings (ui.StringsFor). The runtime's JS twin is
//     core-ui/check LintRuntimeProse. A ScreenTitle or
//     ScreenDescription method returning such a constant is the same
//     shape: the method takes the request ctx, so return phrase.Text(ctx).
//   - nativetext: prose handed to the OS by a desktop shell: the string
//     argument of objc.NSString/NSStringErr, and the title or text of
//     win32.SetWindowText, AppendMenu, MessageBox, CreateWindow and
//     CreateWindowEx. The shell speaks the OS language
//     (desktop.ShellContext), so render a phrase in that context and
//     pass its text. AppKit keys and selectors ("CFBundleShortVersionString")
//     read as no words and stay quiet.
//   - numtext: a number turned into Go's digits (strconv.Itoa,
//     FormatInt, FormatUint, FormatFloat, or fmt.Sprint/Sprintf of plain
//     numbers) where a reader sees it: a value in a params map literal
//     passed to Phrase.Text, Translator.T or i18n's T, Fill, FillContext,
//     NewError or Wrap; the argument of render.Text; a label attribute
//     or reader-facing component field. Phrase params write a number in
//     the reader's locale (1 234 in French, ١٬٢٣٤ in Egyptian Arabic) and
//     pick the plural form from it, so ASCII digits read wrong both ways.
//     Pass the number, or write it with i18n.Number. A machine value a
//     person copies (an id, a port, a code) carries the allow marker.
//   - ctxless: a UI-package component literal whose type has a
//     Ctx context.Context field, left unset in a function that holds a
//     request context: a context.Context parameter or local, a
//     parameter or receiver carrying Ctx (cfg.Ctx), or an *http.Request
//     (r.Context()). The component says its own words (a toggle's
//     label, a form's submit button) through Ctx, so without it they
//     render in English on every locale's page. Meridian's header
//     ThemeToggle and SignOut did, on /es/. A literal bound to a
//     variable whose Ctx the function assigns later, and a positional
//     literal, stay quiet.
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
	"iter"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "rawtext",
	Doc:  "forbids framework responses written outside the i18n catalog: http.Error, i18n.Define off package level or with non-constant text, handler.Errorf/Message, response helpers taking a msg string, prose or err.Error() in error envelopes and raw JSON bodies, English written into markup (render.Text, label attributes, reader-facing component fields), numbers stringified into phrase params or markup, and UI components left without Ctx while a request context is in scope",
	Run:  run,
}

const module = "github.com/DonaldMurillo/gofastr/"

// inScope reports whether path is one of the framework's own packages,
// or Meridian.
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
	// Meridian, the flagship: the app every framework change is checked
	// against says its words through phrases, the way a generated app
	// does, so the pseudo-locale sweep sees every word it shows.
	return rest == "examples/meridian" || strings.HasPrefix(rest, "examples/meridian/")
}

var msgParams = map[string]bool{
	"msg": true, "message": true, "errMsg": true, "errMessage": true, "errorMessage": true,
}

func run(pass *analysis.Pass) (any, error) {
	if !inScope(pass.Pkg.Path()) {
		return nil, nil
	}
	defer localProseCache.Delete(pass)
	defer localNumberCache.Delete(pass)
	defer packageProseCache.Delete(pass)
	forwarders := phraseForwarders(pass)
	texts := textParams(pass)
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		bodies := funcBodies(f)
		ui := uiReporter{pass: pass, specs: specLiterals(pass, f), forwarders: forwarders, textParams: texts}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				checkCall(pass, n, bodies, ui)
			case *ast.CompositeLit:
				checkLiteral(pass, n, ui)
			case *ast.FuncDecl:
				checkParams(pass, n.Type, n.Name.Name)
				checkScreenText(pass, n, ui)
				checkCtxless(pass, n.Type, n.Body, n.Recv, ui)
			case *ast.GenDecl:
				// A func literal at package level (var h = func(w, r) …)
				// belongs to no declared function, so ctxless reads it here;
				// the walk covers the literals nested in it.
				ast.Inspect(n, func(m ast.Node) bool {
					if fl, ok := m.(*ast.FuncLit); ok {
						checkCtxless(pass, fl.Type, fl.Body, nil, ui)
						return false
					}
					return true
				})
			case *ast.FuncLit:
				checkParams(pass, n.Type, "func literal")
			case *ast.AssignStmt:
				checkLabelIndex(pass, n, ui)
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
		reportNumberText(pass, call.Args[0], ui, "a number into the page")
	}
	if args, ok := nativeSinks[pkg][name]; ok {
		for _, i := range args {
			if i < len(call.Args) {
				if s, ok := uiProse(pass, call.Args[i]); ok {
					ui.reportf(call.Args[i], "%s(%q) hands English to the OS: define it as an i18n.Phrase at package level and pass phrase.Text of the shell's context (desktop.ShellContext)", name, s)
				}
			}
		}
	}
	checkPhraseParams(pass, call, ui)
	checkTextArgs(pass, call, ui)
}

// nativeSinks are the desktop shells' calls that hand a string to the OS
// for a person to read (a menu row, a window title, an alert, a tooltip
// through NSString), with the argument positions that carry it. The
// shell renders its words in the OS language, so a constant here is
// English on every machine. Keys and selectors passed to NSString
// ("CFBundleShortVersionString") are not prose and stay quiet. The
// Linux shell has no entry yet: internal/gtk only resolves symbols and
// nothing calls gtk_menu_item_new_with_label or gtk_window_set_title,
// so the wrappers that do add their rows here.
var nativeSinks = map[string]map[string][]int{
	module + "battery/desktop/internal/objc": {
		"NSString": {0}, "NSStringErr": {0},
	},
	module + "battery/desktop/internal/win32": {
		"SetWindowText": {1}, "AppendMenu": {3}, "MessageBox": {1, 2},
		"CreateWindow": {1}, "CreateWindowEx": {2},
	},
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
	// forwarders are this package's helpers that hand their params to
	// a phrase sink (phraseForwarders).
	forwarders map[*types.Func]bool
	// textParams are this package's helpers' string parameters that
	// reach the page (textParams).
	textParams map[*types.Func]map[int]bool
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
	"Help": true,
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

// reWords is two words, with any short ones between ("Choose a value");
// reCapWord one capitalised word.
var (
	reWords   = regexp.MustCompile(`\pL{2,}[,.:;!?)]*(?:\s+\S+)*?\s+\S*\pL{2,}`)
	reCapWord = regexp.MustCompile(`^\p{Lu}\p{Ll}{2,}[.!?…]?$`)
)

// uiProse reports e's constant text when it reads as words for a person.
// A concatenation is prose when one of its constant operands is: "Method
// " + m + " is not allowed" spells English around a value.
// A local variable is prose when the function assigns it prose
// (localProse), and a package-level one when its declaration does
// (packageProse).
func uiProse(pass *analysis.Pass, e ast.Expr) (string, bool) {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
			if s, ok := localProse(pass)[v]; ok {
				return s, true
			}
			if s, ok := packageProse(pass)[v]; ok {
				return s, true
			}
		}
	}
	return constProse(pass, e)
}

// reVerb is a fmt verb with its flags, width and precision.
var reVerb = regexp.MustCompile(`%[-+# 0-9.*\[\]]*[a-zA-Z%]`)

// constProse is uiProse for a constant or a concatenation, no variables
// followed. A fmt.Sprintf is prose when its constant format is, verbs
// removed: "Page %d of %d" spells English around the numbers.
func constProse(pass *analysis.Pass, e ast.Expr) (string, bool) {
	if call, ok := ast.Unparen(e).(*ast.CallExpr); ok && len(call.Args) > 0 {
		if pkg, name := callee(pass, call); pkg == "fmt" && name == "Sprintf" {
			tv, ok := pass.TypesInfo.Types[call.Args[0]]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				return "", false
			}
			// Removed, not replaced by a space: "report%s.csv" is a
			// file name, not the words "report .csv".
			return proseText(reVerb.ReplaceAllString(constant.StringVal(tv.Value), ""))
		}
	}
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return "", false
	}
	if bin, isBin := ast.Unparen(e).(*ast.BinaryExpr); isBin && tv.Value == nil && bin.Op == token.ADD {
		if s, ok := constProse(pass, bin.X); ok {
			return s, true
		}
		return constProse(pass, bin.Y)
	}
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return proseText(constant.StringVal(tv.Value))
}

// proseText reports s, trimmed, when it reads as words for a person.
func proseText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "<>{}=") {
		return "", false // markup, a template or CSS, not a label
	}
	if strings.ToUpper(s) == s {
		return "", false // "GET, POST": tokens, not words
	}
	return s, reWords.MatchString(s) || reCapWord.MatchString(s)
}

// screenTextMethods are the app.ScreenTitler and app.ScreenDescriber
// methods: the tab title, the head's description and the route table.
var screenTextMethods = map[string]bool{"ScreenTitle": true, "ScreenDescription": true}

// checkScreenText reports a ScreenTitle or ScreenDescription method
// returning prose. Both take the request ctx so the words can follow
// the reader; a literal is English on every locale.
func checkScreenText(pass *analysis.Pass, fn *ast.FuncDecl, ui uiReporter) {
	if fn.Recv == nil || fn.Body == nil || !screenTextMethods[fn.Name.Name] {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // its returns are its own
		case *ast.ReturnStmt:
			if len(n.Results) == 1 {
				if s, ok := uiProse(pass, n.Results[0]); ok {
					ui.reportf(n.Results[0], "%s returns %q, English on every locale: define it as an i18n.Phrase at package level and return phrase.Text(ctx)", fn.Name.Name, s)
				}
			}
		}
		return true
	})
}

// checkLabelIndex reports prose set as a label attribute by index,
// attrs["aria-label"] = "Page steps", the map literal's twin.
func checkLabelIndex(pass *analysis.Pass, as *ast.AssignStmt, ui uiReporter) {
	if as.Tok != token.ASSIGN || len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		if !labelIndex(pass, lhs) {
			continue
		}
		k := pass.TypesInfo.Types[lhs.(*ast.IndexExpr).Index]
		if s, ok := uiProse(pass, as.Rhs[i]); ok {
			ui.reportf(as.Rhs[i], "%s %q is English written into the page: define it as an i18n.Phrase at package level and set phrase.Text(ctx), or take it from the component's Strings", constant.StringVal(k.Value), s)
		}
	}
}

// localProseCache holds each pass's localProse map while it runs, and
// localNumberCache its localNumber map.
var localProseCache, localNumberCache sync.Map // *analysis.Pass -> map[*types.Var]string

// localProse maps each function-scoped string variable (parameters
// included) that some assignment or declaration in the package's
// non-test files sets to prose onto the first such text. Any one
// assignment counts: an English default a later branch may replace
// (label := "Close"; if cfg.Label != "" { label = cfg.Label }) is the
// shape this exists to catch.
func localProse(pass *analysis.Pass) map[*types.Var]string {
	return localVars(pass, &localProseCache, false, func(e ast.Expr) (string, bool) { return constProse(pass, e) })
}

// packageProseCache holds each pass's packageProse map while it runs.
var packageProseCache sync.Map // *analysis.Pass -> map[*types.Var]string

// packageProse maps each package-level string variable whose declaration
// in a non-test file sets it to prose onto that text: var closeLabel =
// "Close" read at a sink is the same English as the literal.
func packageProse(pass *analysis.Pass) map[*types.Var]string {
	if m, ok := packageProseCache.Load(pass); ok {
		return m.(map[*types.Var]string)
	}
	out := map[*types.Var]string{}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					v, _ := pass.TypesInfo.Defs[name].(*types.Var)
					if v == nil {
						continue
					}
					if s, ok := constProse(pass, vs.Values[i]); ok {
						out[v] = s
					}
				}
			}
		}
	}
	packageProseCache.Store(pass, out)
	return out
}

// localNumber maps each function-scoped variable declared with number
// text (n := strconv.Itoa(size)) onto the call's name, when no other
// assignment gives it a different value: a local later set to
// i18n.Number(ctx, n) carries the reader's digits by the time it is
// written.
func localNumber(pass *analysis.Pass) map[*types.Var]string {
	return localVars(pass, &localNumberCache, true, func(e ast.Expr) (string, bool) { return numberCall(pass, e) })
}

// localVars maps the package's function-scoped variables, in non-test
// files, onto what classify reports for a value they are given, cached
// per pass. With every unset, any one such assignment or declaration
// counts. With every set, the variable must be declared with such a
// value (:= or var with an initializer) and no assignment, range
// clause or address-of may give it another; += keeps what it holds.
func localVars(pass *analysis.Pass, cache *sync.Map, every bool, classify func(ast.Expr) (string, bool)) map[*types.Var]string {
	if m, ok := cache.Load(pass); ok {
		return m.(map[*types.Var]string)
	}
	out := map[*types.Var]string{}
	declared := map[*types.Var]bool{}
	poisoned := map[*types.Var]bool{}
	local := func(e ast.Expr) *types.Var {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return nil
		}
		v, _ := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if v == nil || v.IsField() || v.Parent() == nil || v.Parent() == pass.Pkg.Scope() {
			return nil
		}
		return v
	}
	poison := func(e ast.Expr) {
		if v := local(e); v != nil {
			poisoned[v] = true
		}
	}
	record := func(lhs, rhs ast.Expr, declares bool) {
		v := local(lhs)
		if v == nil {
			return
		}
		s, ok := classify(rhs)
		switch {
		case !every:
			if _, seen := out[v]; !seen && ok {
				out[v] = s
			}
		case !ok:
			poisoned[v] = true
		case declares:
			declared[v] = true
			if _, seen := out[v]; !seen {
				out[v] = s
			}
		}
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				switch {
				case n.Tok != token.ASSIGN && n.Tok != token.DEFINE:
					// An op-assign (+=) keeps what the variable holds.
				case len(n.Lhs) == len(n.Rhs):
					for i := range n.Lhs {
						record(n.Lhs[i], n.Rhs[i], n.Tok == token.DEFINE)
					}
				default:
					for _, l := range n.Lhs {
						poison(l)
					}
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for i := range n.Names {
						record(n.Names[i], n.Values[i], true)
					}
				} else {
					for _, name := range n.Names {
						poison(name)
					}
				}
			case *ast.RangeStmt:
				if n.Key != nil {
					poison(n.Key)
				}
				if n.Value != nil {
					poison(n.Value)
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					poison(n.X)
				}
			}
			return true
		})
	}
	if every {
		for v := range out {
			if !declared[v] || poisoned[v] {
				delete(out, v)
			}
		}
	}
	cache.Store(pass, out)
	return out
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
			reportNumberText(pass, kv.Value, ui, "a number into "+k)
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
	for i, val := range litFields(lit, st) {
		name, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("json"), ",")
		if name != "error" && name != "message" && name != "title" && name != "detail" {
			continue
		}
		if v := pass.TypesInfo.Types[val]; v.Value != nil && v.Value.Kind() == constant.String &&
			strings.Contains(constant.StringVal(v.Value), " ") {
			pass.Reportf(val.Pos(), "a body's %s is English prose with no code: write it from a Phrase (reason.Text(ctx) beside reason.Code())", name)
		}
	}
}

// litFields yields each element of a struct literal with the index of
// the field it sets, keyed (Title: "x") or positional ({"x", 404}): the
// positional form writes the same bytes and must not slip past.
func litFields(lit *ast.CompositeLit, st *types.Struct) iter.Seq2[int, ast.Expr] {
	return func(yield func(int, ast.Expr) bool) {
		for pos, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				if pos < st.NumFields() && !yield(pos, e) {
					return
				}
				continue
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			for i := range st.NumFields() {
				if st.Field(i).Name() == id.Name {
					if !yield(i, kv.Value) {
						return
					}
					break
				}
			}
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
	for i, val := range litFields(lit, st) {
		f := st.Field(i)
		if !uiField(f.Name()) {
			continue
		}
		if b, ok := f.Type().Underlying().(*types.Basic); !ok || b.Kind() != types.String {
			continue
		}
		if s, ok := uiProse(pass, val); ok {
			ui.reportf(val, "%s: %q is English written into the page: define it as an i18n.Phrase at package level and set phrase.Text(ctx), or take it from the component's Strings", f.Name(), s)
		}
		reportNumberText(pass, val, ui, "a number into "+f.Name())
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
