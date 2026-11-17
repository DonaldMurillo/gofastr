package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
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

var canonicalMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

var reColonParam = regexp.MustCompile(`/:([A-Za-z_][A-Za-z0-9_]*)`)

var stateSegments = map[string]bool{
	"page": true, "sort": true, "sortby": true,
	"orderby": true, "filter": true, "tab": true, "perpage": true,
	"pagesize": true, "offset": true,
}

func runRouting(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic

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
	type routeKey struct {
		pkg     string
		method  string
		pattern string
	}
	seen := map[routeKey][]site{}
	order := []routeKey{}
	byKey := map[routeKey]Route{}
	for _, r := range table.Routes {
		if r.Pattern == "" || strings.Contains(r.Pattern, "{$}") {
			continue
		}
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
	if !rendersUI(p) {
		return nil
	}
	var out []contracts.Diagnostic
	for _, r := range table.Routes {
		for seg := range strings.SplitSeq(strings.Trim(r.Pattern, "/"), "/") {
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

func checkUntested(p *contracts.Pass, table *RouteTable) []contracts.Diagnostic {
	literals := testStringLiterals(p)
	if len(literals) == 0 {
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

func routeMentioned(pattern string, literals []string) bool {
	prefix := pattern
	if i := strings.IndexAny(pattern, "{:"); i >= 0 {
		prefix = pattern[:i]
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
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

func checkPrefixBoundary(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	pkgLits := map[string]map[string]string{}
	conflicted := map[string]map[string]bool{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		dir := path.Dir(f.Rel)
		if pkgLits[dir] == nil {
			pkgLits[dir] = map[string]string{}
		}
		if conflicted[dir] == nil {
			conflicted[dir] = map[string]bool{}
		}
		for name, val := range fileScopedLits(file) {
			if conflicted[dir][name] {
				continue // a third declaration cannot resurrect the name
			}
			if old, ok := pkgLits[dir][name]; ok && old != val {
				conflicted[dir][name] = true
				delete(pkgLits[dir], name)
				continue
			}
			pkgLits[dir][name] = val
		}
	}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		aliases := importAliases(file)
		fileLits := pkgLits[path.Dir(f.Rel)]
		for _, fn := range functionsIn(file) {
			localLits := bodyScopedLits(fn, aliases)
			ast.Inspect(fn.body, func(n ast.Node) bool {
				call, ok := qualifiedCall(n, aliases, "strings", "HasPrefix")
				if !ok || len(call.Args) != 2 {
					return true
				}
				if !pathishExpr(call.Args[0]) {
					return true
				}
				if !unboundedPrefix(call.Args[1], call.Args[0], localLits, fileLits, aliases) {
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

func unboundedPrefix(e ast.Expr, haystack ast.Expr, local localLits, fileLits map[string]string, aliases map[string]string) bool {
	strong := strongPathExpr(haystack)
	switch v := e.(type) {
	case *ast.BasicLit:
		val, ok := stringLit(v)
		return ok && segmentPrefixCandidate(val, strong)
	case *ast.Ident:
		if val, ok := resolveLit(v.Name, local, fileLits); ok {
			return segmentPrefixCandidate(val, strong)
		}
		return true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return true // arithmetic a prefix is not; treat as dynamic
		}
		chain := v
		for {
			inner, ok := chain.Y.(*ast.BinaryExpr)
			if !ok {
				break
			}
			chain = inner
		}
		switch last := chain.Y.(type) {
		case *ast.BasicLit:
			if val, ok := stringLit(last); ok {
				return !strings.HasSuffix(val, "/")
			}
		case *ast.Ident:
			if val, ok := resolveLit(last.Name, local, fileLits); ok {
				return !strings.HasSuffix(val, "/")
			}
			if local.seps[last.Name] {
				return false // root+sep, sep := string(filepath.Separator)
			}
		case *ast.CallExpr:
			if separatorExpr(last, aliases) {
				return false // base+string(os.PathSeparator)
			}
		}
		return true
	}
	return true
}

func separatorExpr(e ast.Expr, aliases map[string]string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "string" {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch aliases[pkg.Name] {
	case "os":
		return sel.Sel.Name == "PathSeparator"
	case "path/filepath":
		return sel.Sel.Name == "Separator"
	}
	return false
}

func resolveLit(name string, local localLits, fileLits map[string]string) (string, bool) {
	if local.dyn[name] {
		return "", false
	}
	if val, ok := local.vals[name]; ok {
		return val, true
	}
	val, ok := fileLits[name]
	return val, ok
}

func segmentPrefixCandidate(val string, strongHaystack bool) bool {
	if val == "" || strings.HasSuffix(val, "/") || strings.HasSuffix(val, ":") {
		return false
	}
	if strings.Contains(val, "/") {
		return true
	}
	return strongHaystack
}

func fileScopedLits(file *ast.File) map[string]string {
	lits := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
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

type localLits struct {
	vals map[string]string
	dyn  map[string]bool
	seps map[string]bool
}

func bodyScopedLits(fn *funcScope, aliases map[string]string) localLits {
	res := localLits{vals: map[string]string{}, dyn: map[string]bool{}, seps: map[string]bool{}}
	isParam := map[string]bool{}
	params := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			for _, name := range field.Names {
				res.dyn[name.Name] = true
				isParam[name.Name] = true
			}
		}
	}
	switch v := fn.node.(type) {
	case *ast.FuncDecl:
		params(v.Type.Params)
	case *ast.FuncLit:
		params(v.Type.Params)
	}
	set := func(name, val string, literal bool) {
		if res.dyn[name] {
			return
		}
		if !literal || res.vals[name] != "" && res.vals[name] != val {
			res.dyn[name] = true
			delete(res.vals, name)
			return
		}
		res.vals[name] = val
	}
	sepEvery := map[string]bool{} // every assignment so far is a separator call
	sepNever := map[string]bool{} // one assignment was not a separator: sticky
	setSep := func(name string, rhs ast.Expr) {
		if isParam[name] {
			return
		}
		if rhs == nil || !separatorExpr(rhs, aliases) {
			sepNever[name] = true
			delete(sepEvery, name)
			return
		}
		if sepNever[name] {
			return
		}
		sepEvery[name] = true
	}
	ast.Inspect(fn.body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GenDecl:
			if v.Tok != token.CONST && v.Tok != token.VAR {
				return true
			}
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				literal := len(vs.Values) == 1 && len(vs.Names) == 1
				var val string
				if literal {
					if lit, ok := vs.Values[0].(*ast.BasicLit); ok {
						val, literal = stringLit(lit)
					} else {
						literal = false
					}
				}
				for _, name := range vs.Names {
					set(name.Name, val, literal)
					oneToOne := len(vs.Values) == 1 && len(vs.Names) == 1
					if oneToOne {
						setSep(name.Name, vs.Values[0])
					} else {
						setSep(name.Name, nil)
					}
				}
			}
		case *ast.AssignStmt:
			var val string
			literal := len(v.Rhs) == 1
			var rhs ast.Expr
			if literal {
				rhs = v.Rhs[0]
				if lit, ok := v.Rhs[0].(*ast.BasicLit); ok {
					val, literal = stringLit(lit)
				} else {
					literal = false
				}
			}
			single := literal && len(v.Lhs) == 1
			oneToOne := len(v.Rhs) == 1 && len(v.Lhs) == 1
			for _, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue // a field or index write, not this local
				}
				set(id.Name, val, single)
				if oneToOne {
					setSep(id.Name, rhs)
				} else {
					setSep(id.Name, nil)
				}
			}
		}
		return true
	})
	for name := range sepEvery {
		res.seps[name] = true
	}
	return res
}

var rePathishName = regexp.MustCompile(`(?i)path|route|url|uri|dir`)

func pathishExpr(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if v.Name == "rel" || rePathishName.MatchString(v.Name) {
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

func strongPathExpr(e ast.Expr) bool {
	found := false
	var scan func(x ast.Expr)
	scan = func(x ast.Expr) {
		if found || x == nil {
			return
		}
		if call, ok := x.(*ast.CallExpr); ok {
			for _, a := range call.Args {
				scan(a)
			}
			return
		}
		ast.Inspect(x, func(n ast.Node) bool {
			if found {
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				for _, a := range c.Args {
					scan(a)
				}
				return false
			}
			if id, ok := n.(*ast.Ident); ok && (id.Name == "rel" || rePathishName.MatchString(id.Name)) {
				found = true
				return false
			}
			return true
		})
	}
	scan(e)
	return found
}
