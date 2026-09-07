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

// ----------------------------------------------------------------------
// GOFASTR1414: dialect twin queries whose WHERE predicates diverge.
// ----------------------------------------------------------------------

// Bug class: sibling SQL queries for Postgres and SQLite whose WHERE
// clauses select different rows. A dialect twin is written twice
// because the spelling differs (FOR UPDATE SKIP LOCKED vs a tx, $n vs
// ?), and the PREDICATES are supposed to be copied verbatim — nothing
// else reviews them against each other, so an atom present in one and
// absent from the other ships silently: on one dialect the query lets
// through exactly the rows the other refuses. The 2026-09-07
// adversarial round found framework/outbox claimDeliveriesSQLite
// missing the `next_attempt_at IS NULL OR next_attempt_at <= $`
// backoff predicate its Postgres twin has (deliveries past their
// retry deadline were re-claimed on SQLite only, so a poison delivery
// looped past its backoff on every SQLite app).
//
// Pairing heuristics, both implemented:
//   - (a) two functions or consts in one package whose names differ
//     only by a Postgres/PG vs SQLite suffix or infix
//     (claimDeliveriesPostgres / claimDeliveriesSQLite);
//   - (b) a switch/if on a dialect value (s.dialect, DialectPostgres,
//     "postgres") with SQL in each arm, including the early-return
//     spelling whose other arm is the code after the if.
//
// Each side's table set is extracted first (every FROM / INTO /
// UPDATE target, sub-selects included, under the same normalization)
// and the WHERE atoms are compared ONLY when the two sets are equal
// or one is a subset of the other. Twins shaped by different tables
// are shaped by different catalogs — pg_tables vs sqlite_master,
// information_schema vs sqlite_master (framework/migrate bulk,
// framework/export_data tableExists) — and owe each other no
// predicate parity: comparing them reports apples against oranges.
// The subset arm keeps the outbox oracle, whose Postgres side wraps
// its SQLite twin's SELECT in an UPDATE over the SAME table.
//
// Only the FIRST SQL-bearing literal of each side is compared (the
// query the function leads with; a twin's write-back UPDATE by
// primary key is not a selection predicate), and every WHERE clause
// of it is pooled — the outbox Postgres claim's predicates live in
// the sub-SELECT under WHERE id IN (SELECT …). Placeholders ($n, ?,
// %s), quoting, whitespace, and single-letter table aliases are
// normalized away; IN-lists of placeholders collapse to one; atoms
// carrying a sub-SELECT are structural (id IN (SELECT …)) and skipped
// rather than compared as text.
//
// The rule does not decide which side is right. It reports the
// divergence; an atom one side lacks is a finding on that side.
//
// Deliberately silent on:
//   - twins whose table sets diverge (different system catalogs):
//     no parity is expected, and the pair is not a finding but a
//     shape the rule refuses to judge;
//   - pairs where either side has no WHERE-bearing statement (DDL
//     twins, INSERT/upsert twins: nothing to compare);
//   - statements whose predicates match after normalization — the
//     overwhelming majority, and the whole point of normalizing;
//   - packages with no pairing shape at all;
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1414) <why>.
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

// dialectQuery is one side of a potential twin pair: the flavor, the
// enclosing name, the first statement's WHERE atoms, and the table
// set those atoms range over.
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
	// branchPairs holds heuristic (b)'s arm pairs, matched by
	// construction: the two arms of one dialect branch never share a
	// declaration name, so the name grouping cannot pair them.
	branchPairs [][2]*dialectQuery
}

// dialectPairs returns the postgres/sqlite query pairs of the package
// by name heuristic (a).
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

// fileDiagnostics compares every pair this file takes part in.
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
			// Twins shaped by DIFFERENT tables are shaped by different
			// catalogs (pg_tables vs sqlite_master): predicate parity
			// is not expected between them, so the comparison stops
			// before it starts. Only an equal or subset table set
			// (the outbox Postgres side adds a sub-select on the SAME
			// table) carries a predicate-parity obligation.
			if !tablesComparable(side.tables, other.tables) {
				continue
			}
			// Atoms the OTHER side enforces and this side lacks are
			// findings on THIS side: the poorer query is the one
			// missing a predicate.
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

// dialectDriftPackage builds the report for one directory: one query
// per dialect-flavored declaration (functions and consts), heuristic
// (a), plus one per arm of a dialect branch, heuristic (b).
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
				// Heuristic (b): a dialect if. The then-arm is the
				// flavor the condition names; the other side is the
				// else-arm, or — the early-return spelling — the code
				// that follows the if.
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
				// The tag is usually the dialect value, but a switch on
				// any value whose CASE LABELS name the dialects
				// (DialectPostgres / DialectSQLite) is the same branch.
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

// queryFromNode reduces one side to its comparable query: the FIRST
// SQL-bearing string literal under node, its first WHERE clause, and
// that clause's normalized atoms. Nil when the side has nothing
// comparable.
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

// statementTables extracts the table names a statement ranges over:
// every FROM / INTO / UPDATE target, sub-selects included, after the
// same placeholder/quoting/case normalization the atoms get (the
// repo's twins build table names with %s verbs, which normalize to ?
// on both sides). Keywords caught by the same shape (DO UPDATE SET…)
// are dropped by name.
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

// reTableTarget matches a table-name position: FROM x, INSERT INTO x,
// UPDATE x — the verb spellings that introduce a table.
var reTableTarget = regexp.MustCompile(`(?:\bfrom|\binto|\bupdate)\s+([a-z_][a-z0-9_$.]*|\?)`)

// sqlKeywords are words the table-target shape can capture that name
// clauses, not tables (ON CONFLICT DO UPDATE SET …).
var sqlKeywords = map[string]bool{
	"set": true, "values": true, "select": true, "where": true, "on": true,
}

// tablesComparable reports whether two twins owe each other predicate
// parity: equal table sets, or one a subset of the other (the outbox
// Postgres claim adds a sub-select on the SAME table its SQLite twin
// selects from). An empty set means extraction saw nothing — compare
// rather than forgive, the atoms are the finding either way.
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

// firstSQLStatement returns the first statement-bearing region of a
// literal: the verb position and the text from it.
func firstSQLStatement(s string) (string, int, bool) {
	loc := reDialectVerb.FindStringIndex(s)
	if loc == nil {
		return "", -1, false
	}
	return s[loc[0]:], loc[0], true
}

// whereAtoms extracts every WHERE clause of the first SQL statement
// in s and pools their normalized top-level atoms. A dialect twin's
// selection predicates often live in a sub-SELECT under an UPDATE …
// WHERE id IN (SELECT … WHERE …) (framework/outbox, battery/webhook):
// the outer WHERE's only atom IS the sub-select — structural, dropped
// — and the predicates the rule exists to compare sit in the inner
// clause. Atoms containing a sub-SELECT are dropped (structural, not
// predicates).
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

// whereClauseAt extracts the WHERE clause starting at the keyword
// index w, scanning depth-aware: the clause ends at a clause stop word
// at the depth WHERE started at, or when the depth drops below it.
// Returns the clause text and its offset.
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

// splitTopLevel splits a WHERE clause on AND/OR at paren depth 0,
// honouring single-quoted literals.
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

// matchWord reports whether s[at:] starts the word w (any case)
// followed by whitespace.
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

// normalizeAtom makes two spellings of one predicate comparable.
func normalizeAtom(raw string) string {
	s := strings.ToLower(raw)
	s = rePlaceholder.ReplaceAllString(s, "?")
	s = reQuotedIdent.ReplaceAllString(s, "")
	s = reAliasDot.ReplaceAllString(s, "")
	s = reINList.ReplaceAllString(s, "in (?)")
	s = reWS.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// dialectNameFlavor reports the dialect a declaration name carries:
// a Postgres/PG vs SQLite token (suffix or infix).
func dialectNameFlavor(name string) string {
	flavor := ""
	for _, t := range splitCamel(name) {
		switch strings.ToLower(t) {
		case "postgres", "pg":
			if flavor == "" {
				flavor = "postgres"
			}
		case "sqlite":
			if flavor == "" {
				flavor = "sqlite"
			}
		}
	}
	return flavor
}

// dialectBaseName strips the dialect tokens from a name so twins land
// on one key: claimDeliveriesPostgres → claimdeliveries.
func dialectBaseName(name string) string {
	var keep []string
	for _, t := range splitCamel(name) {
		switch strings.ToLower(t) {
		case "postgres", "pg", "sqlite":
		default:
			keep = append(keep, strings.ToLower(t))
		}
	}
	return strings.Join(keep, "")
}

// splitCamel splits a name into camelCase tokens.
func splitCamel(name string) []string {
	s := reCamelStep.ReplaceAllString(name, "${1} ${2}")
	return strings.Fields(s)
}

// dialectCondFlavor classifies an if on a dialect value: the flavor
// tag and whether the THEN arm is that flavor.
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
			// `if dialect != DialectSQLite { … }` — the then-arm is
			// the flavor the condition does NOT name.
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

// literalFlavor reports the dialect an operand names: the string
// "postgres"/"sqlite" or an identifier containing one (DialectPostgres,
// dialectSQLite).
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

// touchesDialect reports whether e references a dialect value: an
// identifier or field named dialect (s.dialect, q.dialect, dialect).
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

// switchLabelsDialect reports whether any case label of the switch
// names a dialect.
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

// endsInReturn reports whether the block's last statement returns.
func endsInReturn(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

// followingBlock wraps the statements after node in its enclosing
// block: the early-return spelling's other arm. Nil when node is not
// a direct statement of some block.
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

// unquoteBasicLit renders a string literal's value.
func unquoteBasicLit(lit *ast.BasicLit) string {
	s, _ := stringLit(lit)
	return s
}
