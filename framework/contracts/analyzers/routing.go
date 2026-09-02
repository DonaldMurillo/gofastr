package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "routing",
		Doc:  "Discovers registered routes and checks pattern syntax, duplicates, and test coverage.",
		Rules: []string{
			contracts.RuleDuplicateRoute,
			contracts.RuleColonPathParam,
			contracts.RuleUntestedRoute,
			contracts.RuleStateAsRoute,
			contracts.RuleNonUppercaseVerb,
			contracts.RulePrefixSegmentBoundary,
		},
		Run: runRouting,
	})
}

// canonicalMethods are the verbs ServeMux is given. Anything else in a
// method position is either a typo or a case mistake, both of which
// register cleanly and then never match.
var canonicalMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// reColonParam finds an Express-style parameter: a path segment that
// starts with a colon. Anchored to a slash so a colon inside a segment
// (a matrix parameter, a port in a proxied absolute URL) is not a match.
var reColonParam = regexp.MustCompile(`/:([A-Za-z_][A-Za-z0-9_]*)`)

// stateSegments are path segments that describe DISCRETE LIST state,
// such as sort, page, filter, or tab, rather than a resource. That
// narrowness is the rule's precision: state verbs only, never nouns
// ("order" was removed because `GET /order/{id}` is a shop's resource
// route, not sort state), and nothing from the continuous client-owned
// class (a map's lat/lng/zoom, a chart's range), where a URL is the
// deliberate, shareable artifact and neither a route nor an island is
// the answer. That state lives in the client signal store.
var stateSegments = map[string]bool{
	"page": true, "sort": true, "sortby": true,
	"orderby": true, "filter": true, "tab": true, "perpage": true,
	"pagesize": true, "offset": true,
}

func runRouting(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic

	// The prefix rule needs no route table, so it runs before the
	// Registered gate: a module that registers no routes can still match
	// a path against a prefix with no segment boundary.
	out = append(out, checkPrefixBoundary(p)...)

	table := Routes(p)
	if !table.Registered {
		return out, nil
	}
	out = append(out, checkMethodCase(p, table)...)
	out = append(out, checkColonParams(p, table)...)
	out = append(out, checkDuplicates(p, table)...)
	out = append(out, checkStateRoutes(p, table)...)
	out = append(out, checkUntested(p, table)...)
	return out, nil
}

func checkMethodCase(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		if r.Screen || r.Method == "" || canonicalMethods[r.Method] {
			continue
		}
		upper := strings.ToUpper(r.Method)
		if !canonicalMethods[upper] {
			// Not a case problem, an unrecognised verb entirely. Still
			// worth reporting, with a message that says which it is.
			d := diag(p, contracts.RuleNonUppercaseVerb, r.File, r.Pos,
				fmt.Sprintf("route %s %s uses an unrecognised HTTP method", r.Method, r.Pattern))
			d.Evidence = map[string]string{"method": r.Method, "pattern": r.Pattern}
			out = append(out, d)
			continue
		}
		d := diag(p, contracts.RuleNonUppercaseVerb, r.File, r.Pos,
			fmt.Sprintf("route %q %s registers under a non-uppercase method: every real %s request will get 405",
				r.Method, r.Pattern, upper))
		d.Suggestion = fmt.Sprintf("change %q to %q", r.Method, upper)
		d.Evidence = map[string]string{"method": r.Method, "want": upper, "pattern": r.Pattern}
		if fix := replaceLiteralFix(p, r.File, r.Line, r.Method, upper,
			fmt.Sprintf("uppercase the method to %q", upper)); fix != nil {
			d.Fix = fix
		}
		out = append(out, d)
	}
	return out
}

func checkColonParams(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		// Screens are matched by core-ui's router, where `:id` is the
		// native spelling. Flagging it there would be telling people to
		// break working code.
		if r.Screen {
			continue
		}
		matches := reColonParam.FindAllStringSubmatch(r.RawPattern, -1)
		if len(matches) == 0 {
			continue
		}
		names := make([]string, 0, len(matches))
		braced := r.RawPattern
		for _, m := range matches {
			names = append(names, m[1])
			braced = strings.Replace(braced, "/:"+m[1], "/{"+m[1]+"}", 1)
		}
		d := diag(p, contracts.RuleColonPathParam, r.File, r.Pos, fmt.Sprintf(
			"route %s %s uses `:%s`: ServeMux matches that literally, so requests to a real value 404",
			r.Method, r.RawPattern, names[0]))
		d.Suggestion = fmt.Sprintf("write %q, and read the value with r.PathValue(%q)", braced, names[0])
		d.Evidence = map[string]string{
			"pattern": r.RawPattern, "want": braced, "params": strings.Join(names, ","),
		}
		out = append(out, d)
	}
	return out
}

func checkDuplicates(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	type site struct {
		file string
		line int
	}
	// routeKey is the dedup identity of a registration. A struct, not
	// package+"\x00"+method+" "+pattern: a joined string is ambiguous when
	// a part embeds the separator (gofastrcompositekey).
	type routeKey struct {
		pkg     string
		method  string
		pattern string
	}
	seen := map[routeKey][]site{}
	order := []routeKey{}
	byKey := map[routeKey]Route{}
	for _, r := range table.Routes {
		// An unresolved group prefix makes the full path unknown, so two
		// routes that look identical here may well be distinct. Skip them
		// rather than report a duplicate that is not one.
		if r.Pattern == "" || strings.Contains(r.Pattern, "{$}") {
			continue
		}
		// Scoped by package. A repository holding several apps, such as
		// examples, benchmarks, or test fixtures, has many programs that
		// each serve "/healthz", and none of them collide with each
		// other. Only a second registration in the same package is a
		// real conflict.
		key := routeKey{r.Package, r.Method, r.Pattern}
		if _, ok := seen[key]; !ok {
			order = append(order, key)
			byKey[key] = r
		}
		seen[key] = append(seen[key], site{r.File, r.Line})
	}
	var out []contracts.Diagnostic
	for _, key := range order {
		sites := seen[key]
		if len(sites) < 2 {
			continue
		}
		first := byKey[key]
		others := make([]string, 0, len(sites)-1)
		for _, s := range sites[1:] {
			others = append(others, fmt.Sprintf("%s:%d", s.file, s.line))
		}
		route := first.Method + " " + first.Pattern
		d := diag(p, contracts.RuleDuplicateRoute, first.File, first.Pos,
			fmt.Sprintf("%s is registered %d times: also at %s", route, len(sites), strings.Join(others, ", ")))
		d.Evidence = map[string]string{"route": route, "sites": strings.Join(others, ",")}
		out = append(out, d)
	}
	return out
}

func checkStateRoutes(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	// The rule's premise, that sorting a table is not navigation, is
	// about browsers: scroll position, focus, history entries. A headless
	// API has none of those, and `/orders/page/{n}` is an ordinary REST
	// shape there. Only a module that actually renders UI is speaking
	// the language this rule polices.
	if !rendersUI(p) {
		return nil
	}
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		for seg := range strings.SplitSeq(strings.Trim(r.Pattern, "/"), "/") {
			// Only LITERAL segments carry the rule's evidence. A wildcard
			// named {page} is a resource identifier, a CMS page slug, not
			// pagination; stripping the braces before matching erased
			// exactly that distinction.
			if strings.HasPrefix(seg, "{") {
				continue
			}
			clean := strings.ToLower(strings.Trim(seg, "{}."))
			clean = strings.NewReplacer("-", "", "_", "").Replace(clean)
			if !stateSegments[clean] {
				continue
			}
			d := diag(p, contracts.RuleStateAsRoute, r.File, r.Pos, fmt.Sprintf(
				"route %s %s puts %q in the path: that is in-page state, not navigation",
				r.Method, r.Pattern, seg))
			d.Evidence = map[string]string{"pattern": r.Pattern, "segment": seg}
			out = append(out, d)
			break
		}
	}
	return out
}

// checkUntested is the static half of route coverage: does any test file
// in the module mention this path at all. It is a weaker claim than
// GOFASTR1101 (which proves a request reached the route) and it is worth
// having anyway, because it needs no test run. It catches the route
// added in the same commit as no test whatsoever.
func checkUntested(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	literals := testStringLiterals(p)
	if len(literals) == 0 {
		// No test files at all, or none with string literals. Reporting
		// every route as untested would be technically true and useless.
		// The project has a bigger problem than this rule describes.
		return nil
	}
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		if routeMentioned(r.Pattern, literals) {
			continue
		}
		d := diag(p, contracts.RuleUntestedRoute, r.File, r.Pos,
			fmt.Sprintf("no test file mentions %s", r.Pattern))
		d.Suggestion = fmt.Sprintf("add a test that requests %s: testkit.NewApp gives you an in-process client", r.Pattern)
		d.Evidence = map[string]string{"pattern": r.Pattern, "method": r.Method}
		out = append(out, d)
	}
	return out
}

const testLiteralsKey = "analyzers.testliterals"

// testStringLiterals collects every string literal in the module's test
// files. Literal text is the only signal available without running
// anything, and it is enough: a test that exercises a route names it.
func testStringLiterals(p *contracts.Pass) []string {
	return p.Memo(testLiteralsKey, func() any {
		seen := map[string]bool{}
		for _, f := range p.TestFiles() {
			file, ok := p.AST(f.Rel)
			if !ok {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				expr, isExpr := n.(ast.Expr)
				if !isExpr {
					return true
				}
				if s, litOK := stringLit(expr); litOK && strings.Contains(s, "/") {
					seen[s] = true
				}
				return true
			})
		}
		out := make([]string, 0, len(seen))
		for s := range seen {
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}).([]string)
}

// routeMentioned matches a route pattern against test literals. A pattern
// with parameters is matched on the static prefix before the first brace,
// because the test uses a concrete value where the pattern has a wildcard.
func routeMentioned(pattern string, literals []string) bool {
	prefix := pattern
	if i := strings.IndexAny(pattern, "{:"); i >= 0 {
		prefix = pattern[:i]
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		// The root route. Only an exact "/" (or a query on it) counts.
		// Otherwise every literal path in the suite would match it.
		for _, l := range literals {
			if l == "/" || strings.HasPrefix(l, "/?") {
				return true
			}
		}
		return false
	}
	for _, l := range literals {
		if l == prefix || strings.Contains(l, prefix+"/") || strings.Contains(l, prefix+"?") ||
			strings.HasSuffix(l, prefix) {
			return true
		}
	}
	return false
}

// replaceLiteralFix builds a single-token replacement edit for a quoted
// literal on a known line. It returns nil unless the old text appears
// exactly once on that line. An ambiguous match is not something to
// resolve by guessing.
func replaceLiteralFix(p *contracts.Pass, rel string, line int, oldText, newText, description string) *contracts.SuggestedFix {
	body, ok := p.Source(rel)
	if !ok || line < 1 {
		return nil
	}
	lines := strings.Split(string(body), "\n")
	if line > len(lines) {
		return nil
	}
	offset := 0
	for i := 0; i < line-1; i++ {
		offset += len(lines[i]) + 1
	}
	target := `"` + oldText + `"`
	idx := strings.Index(lines[line-1], target)
	if idx < 0 || strings.Count(lines[line-1], target) != 1 {
		return nil
	}
	start := offset + idx
	return &contracts.SuggestedFix{
		Description: description,
		Edits: []contracts.TextEdit{{
			File:  rel,
			Start: start,
			End:   start + len(target),
			Old:   target,
			New:   `"` + newText + `"`,
		}},
	}
}

// ----------------------------------------------------------------------
// GOFASTR1006: strings.HasPrefix on a path without a segment boundary.
// ----------------------------------------------------------------------

// Bug class: a `/`-separated path or route matched by HasPrefix against a
// prefix that carries no boundary. stability.Classify shipped exactly this
// (probe TestClassifyRequiresSegmentBoundary, fixed e9e50673): manifest
// entries like {"cmd", Provisional} classified every `cmd*` sibling, so a
// package the manifest never named silently inherited a tier and the
// add-it-to-the-manifest gate stayed quiet. framework/uihost's document
// script scope matched the same shape earlier in v0.80. The rule is
// shape-based, not stability-based: any path-like operand matched against
// a boundary-less prefix is the finding.
// Deliberately silent on:
//   - a literal prefix that already ends in "/" ("internal/"), "/" and ""
//     themselves, and — narrowed after the first whole-repo run — any
//     literal or resolvable constant whose value contains no "/" at all
//     ("x_", "dark.", "color-", "v", "&"): those operate on namespace-ish
//     value spaces (YAML keys, token names, versions) where every longer
//     sibling IS the intent, not a confusion. The bug class is about
//     slash-separated trees; a prefix that contains no slash, or one that
//     ends in it, is either bounded or not about segments;
//   - a prefix identifier that resolves to a local or file-scope constant
//     carrying its own boundary (const prefix = "/__gofastr/runtime/"):
//     the boundary is written down one line up;
//   - any prefix built by concatenation at the call (`root+"/"`,
//     `base+sep`): the caller wrote a boundary decision down, and judging
//     a runtime-computed one from the AST would be guessing;
//   - a haystack whose name carries no path/route/prefix semantics
//     (`HasPrefix(hash, "$argon2id$")`) — the shape is a PATH matched by
//     PREFIX, and key/uri/dir naming is the net it uses to catch the
//     ones that don't say "path" out loud;
//   - _test.go and generated files (AppFiles already excludes both).
func checkPrefixBoundary(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		aliases := importAliases(file)
		fileLits := fileScopedLits(file)
		for _, fn := range functionsIn(file) {
			localLits := bodyScopedLits(fn.body)
			ast.Inspect(fn.body, func(n ast.Node) bool {
				call, ok := qualifiedCall(n, aliases, "strings", "HasPrefix")
				if !ok || len(call.Args) != 2 {
					return true
				}
				if !pathishExpr(call.Args[0]) {
					return true
				}
				if !unboundedPrefix(call.Args[1], localLits, fileLits) {
					return true
				}
				hay, needle := exprText(call.Args[0]), exprText(call.Args[1])
				d := diag(p, contracts.RulePrefixSegmentBoundary, f.Rel, call.Pos(),
					fmt.Sprintf("HasPrefix(%s, %s) matches without a segment boundary: the prefix also matches longer siblings (\"cmd\" matches \"cmdline\") — compare equality or HasPrefix(%s, %s+\"/\")",
						hay, needle, hay, needle))
				d.Evidence = map[string]string{"haystack": hay, "prefix": needle}
				out = append(out, d)
				return true
			})
		}
	}
	return out
}

// unboundedPrefix reports whether the prefix argument can name a
// slash-hierarchical tree while carrying no segment boundary. Dynamic
// prefixes (params, fields, range variables) are always treated as
// unbounded: their values are invisible to a parse-only pass, and the
// stability bug lived in exactly one. Literals and resolvable constants
// are judged by their value: no "/" at all is a namespace match, not a
// segment match; a trailing "/" is a boundary.
func unboundedPrefix(e ast.Expr, localLits, fileLits map[string]string) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		val, ok := stringLit(v)
		return ok && segmentPrefixCandidate(val)
	case *ast.Ident:
		if val, ok := localLits[v.Name]; ok {
			return segmentPrefixCandidate(val)
		}
		if val, ok := fileLits[v.Name]; ok {
			return segmentPrefixCandidate(val)
		}
		return true
	case *ast.BinaryExpr:
		// `root+"/"` and every runtime-computed prefix: the boundary
		// decision was written at this call, which is what the rule
		// asks for.
		return false
	}
	// Selectors (r.prefix, cfg.BasePath), calls, index expressions:
	// dynamic.
	return true
}

// segmentPrefixCandidate reports whether a known prefix value names part
// of a slash-separated tree without being bounded to it: it must contain
// an interior or leading slash and not end in one.
func segmentPrefixCandidate(val string) bool {
	return val != "" && strings.Contains(val, "/") && !strings.HasSuffix(val, "/")
}

// fileScopedLits maps package-level const/var names initialized from a
// single string literal.
func fileScopedLits(file *ast.File) map[string]string {
	lits := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 || len(vs.Names) != 1 {
				continue
			}
			if lit, ok := vs.Values[0].(*ast.BasicLit); ok {
				if val, ok := stringLit(lit); ok {
					lits[vs.Names[0].Name] = val
				}
			}
		}
	}
	return lits
}

// bodyScopedLits maps const/var/assignment names in one function body
// (including nested literals) to a single string literal value.
func bodyScopedLits(body ast.Node) map[string]string {
	lits := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GenDecl:
			if v.Tok != token.CONST && v.Tok != token.VAR {
				return true
			}
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 || len(vs.Names) != 1 {
					continue
				}
				if lit, ok := vs.Values[0].(*ast.BasicLit); ok {
					if val, ok := stringLit(lit); ok {
						lits[vs.Names[0].Name] = val
					}
				}
			}
		case *ast.AssignStmt:
			if len(v.Lhs) != 1 || len(v.Rhs) != 1 {
				return true
			}
			id, ok := v.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if lit, ok := v.Rhs[0].(*ast.BasicLit); ok {
				if val, ok := stringLit(lit); ok {
					lits[id.Name] = val
				}
			}
		}
		return true
	})
	return lits
}

// rePathishName names what the first operand usually carries: a path, a
// route, a relative path, a URL, a prefix, a directory, a key. Substring
// match, because real code says importPath, baseURL, routePrefix, docKey.
var rePathishName = regexp.MustCompile(`(?i)path|rel|route|url|uri|prefix|dir|key`)

// pathishExpr reports whether e names or reaches something path-like: an
// identifier in the expression matches rePathishName, or the expression
// selects through a URL field (r.URL.Path, r.URL.Query().Get(...)).
func pathishExpr(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if rePathishName.MatchString(v.Name) {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel != nil && v.Sel.Name == "URL" {
				found = true
			}
		}
		return !found
	})
	return found
}
