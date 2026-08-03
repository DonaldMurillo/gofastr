package analyzers

import (
	"fmt"
	"go/ast"
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

// stateSegments are path segments that describe DISCRETE LIST state —
// sort, page, filter, tab — rather than a resource. That narrowness is
// the rule's precision: state verbs only, never nouns ("order" was
// removed because `GET /order/{id}` is a shop's resource route, not
// sort state), and nothing from the continuous client-owned class (a
// map's lat/lng/zoom, a chart's range), where a URL is the deliberate,
// shareable artifact and neither a route nor an island is the answer —
// that state lives in the client signal store.
var stateSegments = map[string]bool{
	"page": true, "sort": true, "sortby": true,
	"orderby": true, "filter": true, "tab": true, "perpage": true,
	"pagesize": true, "offset": true,
}

func runRouting(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	table := Routes(p)
	if !table.Registered {
		return nil, nil
	}
	var out []contracts.Diagnostic

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
			// Not a case problem — an unrecognised verb entirely. Still
			// worth reporting, with a message that says which it is.
			d := diag(p, contracts.RuleNonUppercaseVerb, r.File, r.Pos,
				fmt.Sprintf("route %s %s uses an unrecognised HTTP method", r.Method, r.Pattern))
			d.Evidence = map[string]string{"method": r.Method, "pattern": r.Pattern}
			out = append(out, d)
			continue
		}
		d := diag(p, contracts.RuleNonUppercaseVerb, r.File, r.Pos,
			fmt.Sprintf("route %q %s registers under a non-uppercase method — every real %s request will get 405",
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
		// native spelling — flagging it there would be telling people to
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
			"route %s %s uses `:%s` — ServeMux matches that literally, so requests to a real value 404",
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
	seen := map[string][]site{}
	order := []string{}
	byKey := map[string]Route{}
	for _, r := range table.Routes {
		// An unresolved group prefix makes the full path unknown, so two
		// routes that look identical here may well be distinct. Skip them
		// rather than report a duplicate that is not one.
		if r.Pattern == "" || strings.Contains(r.Pattern, "{$}") {
			continue
		}
		// Scoped by package. A repository holding several apps — examples,
		// benchmarks, test fixtures — has many programs that each serve
		// "/healthz", and none of them collide with each other. Only a
		// second registration in the same package is a real conflict.
		key := r.Package + "\x00" + r.Method + " " + r.Pattern
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
			fmt.Sprintf("%s is registered %d times — also at %s", route, len(sites), strings.Join(others, ", ")))
		d.Evidence = map[string]string{"route": route, "sites": strings.Join(others, ",")}
		out = append(out, d)
	}
	return out
}

func checkStateRoutes(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	// The rule's premise — sorting a table is not navigation — is about
	// browsers: scroll position, focus, history entries. A headless API
	// has none of those, and `/orders/page/{n}` is an ordinary REST
	// shape there. Only a module that actually renders UI is speaking
	// the language this rule polices.
	if !rendersUI(p) {
		return nil
	}
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		for _, seg := range strings.Split(strings.Trim(r.Pattern, "/"), "/") {
			// Only LITERAL segments carry the rule's evidence. A wildcard
			// named {page} is a resource identifier — a CMS page slug —
			// not pagination; stripping the braces before matching erased
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
				"route %s %s puts %q in the path — that is in-page state, not navigation",
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
// having anyway, because it needs no test run — it catches the route
// added in the same commit as no test whatsoever.
func checkUntested(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	literals := testStringLiterals(p)
	if len(literals) == 0 {
		// No test files at all, or none with string literals. Reporting
		// every route as untested would be technically true and useless —
		// the project has a bigger problem than this rule describes.
		return nil
	}
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		if routeMentioned(r.Pattern, literals) {
			continue
		}
		d := diag(p, contracts.RuleUntestedRoute, r.File, r.Pos,
			fmt.Sprintf("no test file mentions %s", r.Pattern))
		d.Suggestion = fmt.Sprintf("add a test that requests %s — testkit.NewApp gives you an in-process client", r.Pattern)
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
		// The root route. Only an exact "/" (or a query on it) counts —
		// otherwise every literal path in the suite would match it.
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
// exactly once on that line — an ambiguous match is not something to
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
