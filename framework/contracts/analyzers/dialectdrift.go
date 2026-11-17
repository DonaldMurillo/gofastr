package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1414: dialect twin queries whose WHERE predicates diverge.

func ruleDialectDrift(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	v := p.Memo("dialectdrift.package:"+dir, func() any {
		return dialectDriftPackage(p, dir)
	})
	rep, _ := v.(*dialectDriftReport)
	if rep == nil {
		return nil
	}
	return rep.fileDiagnostics(p, rel)
}

type dialectQuery struct {
	rel    string
	flavor string // "postgres" | "sqlite"
	name   string
	lit    *ast.BasicLit
	atoms  []dialectAtom
	tables map[string]bool
}

type dialectAtom struct {
	text string
	off  int
}

type dialectDriftReport struct {
	queries []*dialectQuery
	// branchPairs holds heuristic (b)'s pairs: the two arms of one branch share no declaration name to group by.
	branchPairs [][2]*dialectQuery
}

// dialectPairs returns heuristic (a)'s pairs, by name, plus the branch pairs.
func (r *dialectDriftReport) dialectPairs() [][2]*dialectQuery {
	groups := map[string]map[string][]*dialectQuery{} // base → flavor → queries
	for _, q := range r.queries {
		base := dialectBaseName(q.name)
		if base == "" {
			continue
		}
		if groups[base] == nil {
			groups[base] = map[string][]*dialectQuery{}
		}
		groups[base][q.flavor] = append(groups[base][q.flavor], q)
	}
	var out [][2]*dialectQuery
	out = append(out, r.branchPairs...)
	for _, flavors := range groups {
		for _, pg := range flavors["postgres"] {
			for _, lite := range flavors["sqlite"] {
				out = append(out, [2]*dialectQuery{pg, lite})
			}
		}
	}
	return out
}

func (r *dialectDriftReport) fileDiagnostics(p *contracts.Pass, rel string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, pair := range r.dialectPairs() {
		for _, side := range pair {
			other := pair[0]
			if side == pair[0] {
				other = pair[1]
			}
			if side.rel != rel || len(side.atoms) == 0 || len(other.atoms) == 0 {
				continue
			}
			if !tablesComparable(side.tables, other.tables) {
				continue
			}
			// An atom the other side has and this side lacks is a finding on this side.
			have := map[string]bool{}
			for _, a := range side.atoms {
				have[a.text] = true
			}
			for _, a := range other.atoms {
				if have[a.text] {
					continue
				}
				out = append(out, diag(p, contracts.RuleDialectDrift, rel, side.lit.Pos(),
					fmt.Sprintf("%s's WHERE lacks the predicate its %s twin %s enforces (%q): dialect twins must select the same rows, and an atom one side refuses is a row the other lets through — decide which side is right and copy the predicate across (or annotate //gofastr:allow(GOFASTR1414) <why> when the difference is a real dialect capability)",
						side.name, other.flavorName(), other.name, a.text)))
			}
		}
	}
	return out
}

func (q *dialectQuery) flavorName() string {
	if q.flavor == "postgres" {
		return "Postgres"
	}
	return "SQLite"
}

func dialectDriftPackage(p *contracts.Pass, dir string) *dialectDriftReport {
	rep := &dialectDriftReport{}
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for _, fn := range functionsIn(file) {
			if decl, ok := fn.node.(*ast.FuncDecl); ok && decl.Name != nil {
				flavor := dialectNameFlavor(decl.Name.Name)
				if flavor == "" {
					continue
				}
				if q := queryFromNode(fn.body, flavor, decl.Name.Name, f.Rel); q != nil {
					rep.queries = append(rep.queries, q)
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.GenDecl:
				for _, spec := range v.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Values) == 0 {
						continue
					}
					for _, name := range vs.Names {
						flavor := dialectNameFlavor(name.Name)
						if flavor == "" {
							continue
						}
						if q := queryFromNode(vs.Values[0], flavor, name.Name, f.Rel); q != nil {
							rep.queries = append(rep.queries, q)
						}
					}
				}
			case *ast.IfStmt:
				// Heuristic (b): a dialect if; the other arm is the else, or the code after an if that returns.
				cond, pgThen := dialectCondFlavor(v)
				if cond == "" {
					return true
				}
				var then, other ast.Stmt = v.Body, v.Else
				if !pgThen {
					then, other = other, v.Body // nil then-arm: nothing to pair
					if then == nil {
						return true
					}
				}
				if other == nil && endsInReturn(v.Body) {
					other = followingBlock(n, file)
				}
				if other == nil {
					return true
				}
				thenFlavor, otherFlavor := "postgres", "sqlite"
				if !pgThen {
					thenFlavor, otherFlavor = "sqlite", "postgres"
				}
				thenQ := queryFromNode(then, thenFlavor, "dialect branch", f.Rel)
				otherQ := queryFromNode(other, otherFlavor, "dialect branch's other arm", f.Rel)
				if thenQ != nil {
					rep.queries = append(rep.queries, thenQ)
				}
				if otherQ != nil {
					rep.queries = append(rep.queries, otherQ)
				}
				if thenQ != nil && otherQ != nil {
					rep.branchPairs = append(rep.branchPairs, [2]*dialectQuery{thenQ, otherQ})
				}
			case *ast.SwitchStmt:
				if !touchesDialect(v.Tag) && !switchLabelsDialect(v) {
					return true
				}
				var pgArm, liteArm ast.Node
				for _, cl := range v.Body.List {
					cc, ok := cl.(*ast.CaseClause)
					if !ok || cc.List == nil {
						continue // default: never a dialect twin
					}
					flavor := ""
					for _, e := range cc.List {
						if f := literalFlavor(e); f != "" {
							flavor = f
							break
						}
					}
					switch flavor {
					case "postgres":
						if pgArm == nil {
							pgArm = cc
						}
					case "sqlite":
						if liteArm == nil {
							liteArm = cc
						}
					}
				}
				if pgArm == nil || liteArm == nil {
					return true
				}
				pgQ := queryFromNode(pgArm, "postgres", "dialect switch", f.Rel)
				liteQ := queryFromNode(liteArm, "sqlite", "dialect switch", f.Rel)
				if pgQ != nil {
					rep.queries = append(rep.queries, pgQ)
				}
				if liteQ != nil {
					rep.queries = append(rep.queries, liteQ)
				}
				if pgQ != nil && liteQ != nil {
					rep.branchPairs = append(rep.branchPairs, [2]*dialectQuery{pgQ, liteQ})
				}
			}
			return true
		})
	}
	return rep
}

// queryFromNode reduces one side to its first SQL-bearing literal, its pooled WHERE atoms and its table set.
func queryFromNode(node ast.Node, flavor, name, rel string) *dialectQuery {
	var lit *ast.BasicLit
	ast.Inspect(node, func(n ast.Node) bool {
		if lit != nil {
			return false
		}
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if _, _, ok := firstSQLStatement(unquoteBasicLit(bl)); ok {
				lit = bl
				return false
			}
		}
		return true
	})
	if lit == nil {
		return nil
	}
	atoms := whereAtoms(unquoteBasicLit(lit))
	if len(atoms) == 0 {
		return nil
	}
	return &dialectQuery{rel: rel, flavor: flavor, name: name, lit: lit, atoms: atoms, tables: statementTables(unquoteBasicLit(lit))}
}

func statementTables(s string) map[string]bool {
	out := map[string]bool{}
	stmt, _, ok := firstSQLStatement(s)
	if !ok {
		return out
	}
	normalized := strings.ToLower(stmt)
	normalized = rePlaceholder.ReplaceAllString(normalized, "?")
	normalized = reQuotedIdent.ReplaceAllString(normalized, "")
	normalized = reWS.ReplaceAllString(normalized, " ")
	for _, m := range reTableTarget.FindAllStringSubmatch(normalized, -1) {
		name := m[1]
		if sqlKeywords[name] {
			continue
		}
		out[name] = true
	}
	return out
}

var reTableTarget = regexp.MustCompile(`(?:\bfrom|\binto|\bupdate)(?:\s+only\b)?\s+([a-z_][a-z0-9_$.]*|\?)`)

var sqlKeywords = map[string]bool{
	"set": true, "values": true, "select": true, "where": true, "on": true,
}

func tablesComparable(a, b map[string]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	subset := func(x, y map[string]bool) bool {
		for t := range x {
			if !y[t] {
				return false
			}
		}
		return true
	}
	return subset(a, b) || subset(b, a)
}

var (
	reDialectVerb = regexp.MustCompile(`(?i)\b(?:select|insert|update|delete|with)\b`)
	reClauseStop  = regexp.MustCompile(`(?i)^[\s;]*(?:order|group|limit|offset|having|returning|for|union)\b|^;`)
	rePlaceholder = regexp.MustCompile(`(?:\$\d+|%[sdv])`)
	reAliasDot    = regexp.MustCompile(`\b[a-z]\.`)
	reQuotedIdent = regexp.MustCompile("[\"`]")
	reWS          = regexp.MustCompile(`\s+`)
	reINList      = regexp.MustCompile(`(?i)in\s*\(\s*\?(?:\s*,\s*\?)*\s*\)`)
	reCamelStep   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// firstSQLStatement returns the first verb that opens a statement (at the start, after a newline, "(" or ";") and is followed by SQL.
func firstSQLStatement(s string) (string, int, bool) {
	for _, loc := range reDialectVerb.FindAllStringIndex(s, -1) {
		if !statementBoundary(s[:loc[0]]) || !reSQLShape.MatchString(s[loc[0]:]) {
			continue
		}
		return s[loc[0]:], loc[0], true
	}
	return "", -1, false
}

func statementBoundary(before string) bool {
	t := strings.TrimRight(before, " \t")
	if t == "" {
		return true
	}
	switch t[len(t)-1] {
	case '\n', '\r', '(', ';':
		return true
	}
	return false
}

var reSQLShape = regexp.MustCompile(`(?is)^(?:select\s.*\bfrom\b|select\s+[\w.]+\s*\(|update\s+(?:only\s+)?\S+(?:\s+(?:as\s+)?\w+)?\s+set\b|delete\s+from\b|insert\s+(?:or\s+\w+\s+|ignore\s+)?into\b|with\s+(?:recursive\s+)?\S+\s+as\s*\(|(?:select|update|delete|insert|with)\s*$)`)

func whereAtoms(s string) []dialectAtom {
	stmt, _, ok := firstSQLStatement(s)
	if !ok {
		return nil
	}
	var out []dialectAtom
	lower := strings.ToLower(stmt)
	from := 0
	for {
		w := strings.Index(lower[from:], "where")
		if w < 0 {
			break
		}
		w += from
		clause, off := whereClauseAt(stmt, w)
		for _, raw := range splitTopLevel(clause) {
			a := normalizeAtom(raw.text)
			if a == "" || strings.Contains(a, " select ") || strings.HasPrefix(a, "select ") {
				continue
			}
			out = append(out, dialectAtom{text: a, off: off + raw.off})
		}
		from = w + 5
	}
	return out
}

func whereClauseAt(stmt string, w int) (string, int) {
	depth := 0
	for i := 0; i < w; i++ {
		if stmt[i] == '(' {
			depth++
		}
		if stmt[i] == ')' {
			depth--
		}
	}
	startDepth := depth
	i := w
	for i < len(stmt) {
		switch {
		case stmt[i] == '\'': // skip a string literal
			j := i + 1
			for j < len(stmt) && stmt[j] != '\'' {
				j++
			}
			i = j + 1
			continue
		case stmt[i] == '(':
			depth++
		case stmt[i] == ')':
			depth--
			if depth < startDepth {
				return strings.TrimSpace(stmt[w+5 : i]), w + 5
			}
		}
		if depth == startDepth && reClauseStop.MatchString(stmt[i:]) {
			return strings.TrimSpace(stmt[w+5 : i]), w + 5
		}
		i++
	}
	return strings.TrimSpace(stmt[w+5:]), w + 5
}

type atomSpan struct {
	text string
	off  int
}

func splitTopLevel(where string) []atomSpan {
	var out []atomSpan
	depth, start := 0, 0
	flush := func(end int) {
		if t := strings.TrimSpace(where[start:end]); t != "" {
			out = append(out, atomSpan{text: t, off: start})
		}
	}
	for i := 0; i < len(where); i++ {
		switch where[i] {
		case '\'':
			j := i + 1
			for j < len(where) && where[j] != '\'' {
				j++
			}
			i = j
			continue
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		sep := 0
		if matchWord(where, i, "and") {
			sep = 3
		} else if matchWord(where, i, "or") {
			sep = 2
		}
		if sep == 0 {
			continue
		}
		flush(i)
		i += sep
		start = i + 1
	}
	flush(len(where))
	return out
}

func matchWord(s string, at int, w string) bool {
	if at+len(w) >= len(s) {
		return false
	}
	if !strings.EqualFold(s[at:at+len(w)], w) {
		return false
	}
	switch s[at+len(w)] {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func normalizeAtom(raw string) string {
	s := strings.ToLower(raw)
	s = rePlaceholder.ReplaceAllString(s, "?")
	s = reQuotedIdent.ReplaceAllString(s, "")
	s = reAliasDot.ReplaceAllString(s, "")
	s = reINList.ReplaceAllString(s, "in (?)")
	s = reWS.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func dialectNameFlavor(name string) string {
	for _, t := range dialectTokens(name) {
		if f := dialectTokenFlavor(t); f != "" {
			return f
		}
	}
	return ""
}

func dialectTokenFlavor(t string) string {
	switch t {
	case "postgres", "postgresql", "pg":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	}
	return ""
}

func dialectTokens(name string) []string {
	var raw []string
	for _, part := range strings.Split(name, "_") {
		raw = append(raw, splitCamel(part)...)
	}
	out := make([]string, 0, len(raw))
	for i := 0; i < len(raw); i++ { // classic loop: the body consumes a pair
		t := strings.ToLower(raw[i])
		if t == "postgre" && i+1 < len(raw) && strings.ToLower(raw[i+1]) == "sql" {
			t = "postgresql"
			i++
		}
		out = append(out, t)
	}
	return out
}

func dialectBaseName(name string) string {
	var keep []string
	for _, t := range dialectTokens(name) {
		if dialectTokenFlavor(t) == "" {
			keep = append(keep, t)
		}
	}
	return strings.Join(keep, "")
}

func splitCamel(name string) []string {
	s := reCamelStep.ReplaceAllString(name, "${1} ${2}")
	return strings.Fields(s)
}

func dialectCondFlavor(v *ast.IfStmt) (string, bool) {
	if !touchesDialect(v.Cond) {
		return "", false
	}
	if cmp, ok := v.Cond.(*ast.BinaryExpr); ok {
		switch cmp.Op.String() {
		case "==":
			if f := literalFlavor(cmp.X); f != "" {
				return f, true
			}
			if f := literalFlavor(cmp.Y); f != "" {
				return f, true
			}
		case "!=":
			if f := literalFlavor(cmp.X); f != "" {
				return otherFlavor(f), true
			}
			if f := literalFlavor(cmp.Y); f != "" {
				return otherFlavor(f), true
			}
		}
	}
	return "", false
}

func literalFlavor(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		s, ok := stringLit(v)
		if !ok {
			return ""
		}
		switch strings.ToLower(s) {
		case "postgres", "postgresql", "pg":
			return "postgres"
		case "sqlite", "sqlite3":
			return "sqlite"
		}
	case *ast.Ident:
		return dialectNameFlavor(v.Name)
	case *ast.SelectorExpr:
		return dialectNameFlavor(v.Sel.Name)
	case *ast.ParenExpr:
		return literalFlavor(v.X)
	}
	return ""
}

func otherFlavor(f string) string {
	if f == "postgres" {
		return "sqlite"
	}
	return "postgres"
}

func touchesDialect(e ast.Expr) bool {
	if e == nil {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if strings.EqualFold(v.Name, "dialect") {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel != nil && strings.EqualFold(v.Sel.Name, "dialect") {
				found = true
			}
		}
		return !found
	})
	return found
}

func switchLabelsDialect(v *ast.SwitchStmt) bool {
	if v.Body == nil {
		return false
	}
	for _, cl := range v.Body.List {
		cc, ok := cl.(*ast.CaseClause)
		if !ok || cc.List == nil {
			continue
		}
		for _, e := range cc.List {
			if literalFlavor(e) != "" {
				return true
			}
		}
	}
	return false
}

func endsInReturn(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

func followingBlock(node ast.Node, file *ast.File) ast.Stmt {
	var after *ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if after != nil {
			return false
		}
		b, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, stmt := range b.List {
			if stmt == node && i+1 < len(b.List) {
				rest := append([]ast.Stmt(nil), b.List[i+1:]...)
				after = &ast.BlockStmt{List: rest, Lbrace: rest[0].Pos(), Rbrace: b.Rbrace}
				return false
			}
		}
		return true
	})
	if after == nil {
		return nil // a typed-nil *ast.BlockStmt would defeat the caller's nil check
	}
	return after
}

func unquoteBasicLit(lit *ast.BasicLit) string {
	s, _ := stringLit(lit)
	return s
}
