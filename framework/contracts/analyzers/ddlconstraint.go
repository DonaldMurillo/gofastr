package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ----------------------------------------------------------------------
// GOFASTR1416: an inline constraint that never runs, or runs twice.
// ----------------------------------------------------------------------

// Bug class: a store that creates its own table with CREATE TABLE IF NOT
// EXISTS and declares a constraint INSIDE that statement. Two ways it
// goes wrong, both found in battery/auth's EntitySessionStore:
//
//   - the statement never runs. The documented wiring declares the same
//     table as an entity (app.Entity("sessions",
//     SessionEntityConfig())), so AutoMigrate creates it first and the
//     store's CREATE TABLE IF NOT EXISTS is a no-op: `token TEXT UNIQUE`
//     never existed, every cookie lookup scanned the table, and token
//     uniqueness was not enforced (0550be96d);
//   - it runs beside another spelling. The fix for that added CREATE
//     UNIQUE INDEX IF NOT EXISTS on token, and SessionEntityFields marked
//     token Unique, so a table the store created carried the inline
//     UNIQUE plus the standalone index, and an AutoMigrate-created one
//     the entity's index plus the standalone one: two unique indexes on
//     one column, both maintained on every insert (dbe11ba19).
//
// Shape, per package (tables resolve the way GOFASTR1415 resolves them:
// literal, constant, receiver field, quoting wrapper, a helper or local
// closure that returns the name):
//
//   - (a) a column declared UNIQUE or REFERENCES inside a CREATE TABLE
//     IF NOT EXISTS (inline on the column, or a table-level UNIQUE (…) /
//     FOREIGN KEY (…) REFERENCES), for a table the package also declares
//     as an entity: a field list (a []schema.Field literal, or a literal
//     of elements keyed Name + Type: schema.X) every one of whose two or
//     more field names is a column of that CREATE TABLE;
//   - (b) a single-column CREATE UNIQUE INDEX on (table, column) where
//     the same column is also declared UNIQUE inside a CREATE TABLE of
//     that table, or marked Unique: true in an entity field list that
//     matches it. The finding lands on the redundant inline / field
//     spelling and names the index.
//
// Fix spelling: declare the constraint where it runs whoever made the
// table, once: a standalone CREATE [UNIQUE] INDEX IF NOT EXISTS after the
// CREATE TABLE (EnsureSchema's idx_<t>_token), and no UNIQUE inline or
// on the entity field beside it.
//
// Deliberately silent on:
//   - columns spelled through a format verb (`%s TEXT UNIQUE`, the user
//     table's configurable email column): the rule reads only names it
//     can see;
//   - PRIMARY KEY, which AutoMigrate and the store spell alike;
//   - a CREATE TABLE (no IF NOT EXISTS) — it runs or fails loudly — for
//     arm (a); and tables no entity field list in the package matches;
//   - partial unique indexes (… WHERE status = 'queued') and multi-column
//     ones: neither is the same constraint as a column's UNIQUE;
//   - an entity field marked Unique with an inline UNIQUE and no
//     standalone index: whichever creates the table, one index exists;
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1416) <why>.
func ruleDDLConstraint(p *contracts.Pass, rel string, _ *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	v := p.Memo("ddlconstraint.package:"+dir, func() any {
		return ddlConstraintPackage(p, dir)
	})
	pkg, _ := v.(*dcPkg)
	if pkg == nil {
		return nil
	}
	var out []contracts.Diagnostic
	for _, t := range pkg.tables {
		if t.rel != rel {
			continue
		}
		if t.ifNotExists && len(t.constrained) > 0 {
			if fl := pkg.matchingFields(t); fl != nil {
				cols := make([]string, 0, len(t.constrained))
				for c, kind := range t.constrained {
					cols = append(cols, c+" "+kind)
				}
				sort.Strings(cols)
				out = append(out, diag(p, contracts.RuleDDLConstraint, rel, t.pos, fmt.Sprintf(
					"%s declares %s inside CREATE TABLE IF NOT EXISTS, and this package also declares the table as an entity (the field list at %s): when AutoMigrate creates the table first this statement is a no-op and the constraint never exists — declare it where it runs whoever made the table (a standalone CREATE [UNIQUE] INDEX IF NOT EXISTS after the CREATE TABLE), or annotate //gofastr:allow(GOFASTR1416) <why>",
					t.display, strings.Join(cols, ", "), dcAt(p, fl.pos))))
			}
		}
		for _, c := range sortedKeys(t.unique) {
			if ix := pkg.uniqueIndexOn(t.refs, c); ix != nil {
				out = append(out, diag(p, contracts.RuleDDLConstraint, rel, t.pos, fmt.Sprintf(
					"%s declares %s UNIQUE inside its CREATE TABLE and %s creates a unique index on the same column: a table this statement creates carries two unique indexes on %s, both maintained on every insert — keep the standalone index and drop the inline UNIQUE, or annotate //gofastr:allow(GOFASTR1416) <why>",
					t.display, c, ix.where(p), c)))
			}
		}
	}
	for _, fl := range pkg.fields {
		if fl.rel != rel {
			continue
		}
		for _, c := range sortedKeys(fl.unique) {
			for _, t := range pkg.tables {
				if !fl.matches(t) {
					continue
				}
				if ix := pkg.uniqueIndexOn(t.refs, c); ix != nil {
					out = append(out, diag(p, contracts.RuleDDLConstraint, rel, fl.unique[c], fmt.Sprintf(
						"field %s is marked Unique in an entity field list for %s, and %s creates a unique index on the same column: an AutoMigrate-created table carries the entity's unique index and the standalone one, both maintained on every insert — keep the standalone index and drop Unique from the field, or annotate //gofastr:allow(GOFASTR1416) <why>",
						c, t.display, ix.where(p))))
					break
				}
			}
		}
	}
	return out
}

type dcTable struct {
	rel         string
	pos         token.Pos
	display     string
	refs        []ecRef
	ifNotExists bool
	columns     map[string]bool
	unique      map[string]bool   // column → declared UNIQUE in the statement
	constrained map[string]string // column → "UNIQUE" / "REFERENCES"
}

type dcIndex struct {
	rel  string
	pos  token.Pos
	refs []ecRef
	col  string
}

func (ix *dcIndex) where(p *contracts.Pass) string {
	return "the CREATE UNIQUE INDEX at " + dcAt(p, ix.pos)
}

type dcFields struct {
	rel    string
	pos    token.Pos
	names  map[string]bool
	unique map[string]token.Pos
}

// matches: every one of the list's two or more field names is a column
// the CREATE TABLE spells.
func (fl *dcFields) matches(t *dcTable) bool {
	if len(fl.names) < 2 {
		return false
	}
	for n := range fl.names {
		if !t.columns[n] {
			return false
		}
	}
	return true
}

type dcPkg struct {
	tables  []*dcTable
	indexes []*dcIndex
	fields  []*dcFields
}

func (pkg *dcPkg) matchingFields(t *dcTable) *dcFields {
	for _, fl := range pkg.fields {
		if fl.matches(t) {
			return fl
		}
	}
	return nil
}

func (pkg *dcPkg) uniqueIndexOn(refs []ecRef, col string) *dcIndex {
	for _, ix := range pkg.indexes {
		if ix.col == col && ecCovered(refs, [][]ecRef{ix.refs}) {
			return ix
		}
	}
	return nil
}

var (
	reCreateTableINE  = regexp.MustCompile(`(?is)\bCREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?`)
	reCreateUniqueIdx = regexp.MustCompile(`(?is)\bCREATE\s+UNIQUE\s+INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?`)
	reIndexOn         = regexp.MustCompile(`(?is)\bON\s+(?:ONLY\s+)?`)
	reWordUnique      = regexp.MustCompile(`(?i)\bUNIQUE\b`)
	reWordReferences  = regexp.MustCompile(`(?i)\bREFERENCES\b`)
	reTableUnique     = regexp.MustCompile(`(?is)^(?:CONSTRAINT\s+\S+\s+)?UNIQUE\s*\(([^)]*)\)`)
	reTableForeign    = regexp.MustCompile(`(?is)^(?:CONSTRAINT\s+\S+\s+)?FOREIGN\s+KEY\s*\(([^)]*)\)`)
)

func ddlConstraintPackage(p *contracts.Pass, dir string) *dcPkg {
	pkv := p.Memo("erasecover.package:"+dir, func() any {
		return eraseCoverPackage(p, dir)
	})
	pk, _ := pkv.(*ecPkg)
	if pk == nil {
		return nil
	}
	pkg := &dcPkg{}
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		schemaAlias := ""
		for alias, ip := range importAliases(file) {
			if strings.HasSuffix(ip, "core/schema") {
				schemaAlias = alias
			}
		}
		for _, d := range file.Decls {
			fn, _ := d.(*ast.FuncDecl)
			ecEachSQLString(d, func(root ast.Expr, text string, holes, args []ast.Expr) {
				pkg.parseTables(pk, f.Rel, root, text, holes, args, fn)
				pkg.parseIndexes(pk, f.Rel, root, text, holes, args, fn)
			})
			if schemaAlias != "" {
				pkg.collectFields(f.Rel, d, schemaAlias)
			}
		}
	}
	return pkg
}

func (pkg *dcPkg) parseTables(pk *ecPkg, rel string, root ast.Expr, text string, holes, args []ast.Expr, fn *ast.FuncDecl) {
	for _, m := range reCreateTableINE.FindAllStringSubmatchIndex(text, -1) {
		display, refs, at, ok := pk.tableNameAt(text, m[1], holes, args, fn)
		if !ok || len(refs) == 0 {
			continue
		}
		body, ok := ecColumnList(text, at)
		if !ok {
			continue
		}
		t := &dcTable{
			rel: rel, pos: ecPosIn(root, m[0]), display: display, refs: refs,
			ifNotExists: m[2] >= 0,
			columns:     map[string]bool{}, unique: map[string]bool{}, constrained: map[string]string{},
		}
		for _, item := range dcSplitTop(body) {
			item = strings.TrimSpace(item)
			if sm := reTableUnique.FindStringSubmatch(item); sm != nil {
				if cols := dcColumns(sm[1]); len(cols) == 1 {
					t.unique[cols[0]] = true
					t.constrained[cols[0]] = "UNIQUE"
				}
				continue
			}
			if sm := reTableForeign.FindStringSubmatch(item); sm != nil {
				for _, c := range dcColumns(sm[1]) {
					t.constrained[c] = "REFERENCES"
				}
				continue
			}
			fs := strings.Fields(item)
			if len(fs) == 0 || strings.ContainsAny(fs[0], "%"+ecHole) {
				continue
			}
			col := strings.ToLower(strings.Trim(fs[0], "\"`"))
			switch col {
			case "constraint", "primary", "check", "exclude", "unique", "foreign":
				continue
			}
			t.columns[col] = true
			def := item[len(fs[0]):]
			if reWordUnique.MatchString(def) {
				t.unique[col] = true
				t.constrained[col] = "UNIQUE"
			} else if reWordReferences.MatchString(def) {
				t.constrained[col] = "REFERENCES"
			}
		}
		pkg.tables = append(pkg.tables, t)
	}
}

func (pkg *dcPkg) parseIndexes(pk *ecPkg, rel string, root ast.Expr, text string, holes, args []ast.Expr, fn *ast.FuncDecl) {
	for _, m := range reCreateUniqueIdx.FindAllStringIndex(text, -1) {
		open := strings.IndexByte(text[m[1]:], '(')
		if open < 0 {
			continue
		}
		open += m[1]
		closeAt := strings.IndexByte(text[open:], ')')
		if closeAt < 0 {
			continue
		}
		closeAt += open
		cols := dcColumns(text[open+1 : closeAt])
		if len(cols) != 1 {
			continue // composite or expression: not a column's UNIQUE
		}
		tail := text[closeAt+1:]
		if i := strings.IndexByte(tail, ';'); i >= 0 {
			tail = tail[:i]
		}
		if reWhereWord.MatchString(tail) {
			continue // partial
		}
		seg := text[m[1]:open]
		var refs []ecRef
		if on := reIndexOn.FindStringIndex(seg); on != nil {
			if _, r, _, ok := pk.tableNameAt(text, m[1]+on[1], holes, args, fn); ok {
				refs = r
			}
		} else {
			// The name and the ON clause ride one slot (query.IndexOn):
			// every operand of the slot is a candidate identity.
			for _, e := range dcSlots(text, m[1], open, holes, args) {
				refs = append(refs, pk.deepRefs(e, fn, 0)...)
			}
		}
		if len(refs) == 0 {
			continue
		}
		pkg.indexes = append(pkg.indexes, &dcIndex{rel: rel, pos: ecPosIn(root, m[0]), refs: refs, col: cols[0]})
	}
}

// dcSlots returns the expressions behind the verb slots and holes in
// text[from:to].
func dcSlots(text string, from, to int, holes, args []ast.Expr) []ast.Expr {
	var out []ast.Expr
	for i := from; i < to; i++ {
		switch {
		case text[i] == '%':
			if i+1 < len(text) && text[i+1] == '%' {
				i++
				continue
			}
			if idx, _ := ecVerbIndex(text, i); idx >= 0 && idx < len(args) {
				out = append(out, args[idx])
			}
		case strings.HasPrefix(text[i:], ecHole):
			end := strings.Index(text[i+1:], ecHole)
			if end < 0 {
				return out
			}
			if n, err := strconv.Atoi(text[i+1 : i+1+end]); err == nil && n < len(holes) {
				out = append(out, holes[n])
			}
			i += end + 1
		}
	}
	return out
}

// deepRefs resolves e and every operand inside it, following a call of
// a local closure into the literal's returns (EnsureSchema's
// on := func(suffix string) string { return query.IndexOn(…) }).
func (pk *ecPkg) deepRefs(e ast.Expr, fn *ast.FuncDecl, depth int) []ecRef {
	if e == nil || depth > ecMaxDepth {
		return nil
	}
	out := pk.resolve(e, ecCtx{fn: fn}, 0)
	switch v := e.(type) {
	case *ast.ParenExpr:
		out = append(out, pk.deepRefs(v.X, fn, depth+1)...)
	case *ast.CallExpr:
		for _, a := range v.Args {
			out = append(out, pk.deepRefs(a, fn, depth+1)...)
		}
		if id, ok := v.Fun.(*ast.Ident); ok && fn != nil {
			if defs, ok := ecLocalDefs(fn.Body, id.Name); ok {
				for _, d := range defs {
					lit, ok := d.(*ast.FuncLit)
					if !ok {
						continue
					}
					ast.Inspect(lit.Body, func(n ast.Node) bool {
						if r, ok := n.(*ast.ReturnStmt); ok && len(r.Results) > 0 {
							out = append(out, pk.deepRefs(r.Results[0], fn, depth+1)...)
						}
						return true
					})
				}
			}
		}
	}
	var kept []ecRef
	for _, r := range out {
		if r.key != "" || len(r.names) > 0 {
			kept = append(kept, r)
		}
	}
	return kept
}

// collectFields records the entity field lists a declaration spells: a
// composite literal whose elements are field literals keyed Name (a
// string) and Type (schema.X).
func (pkg *dcPkg) collectFields(rel string, d ast.Decl, schemaAlias string) {
	ast.Inspect(d, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || len(lit.Elts) == 0 {
			return true
		}
		fl := &dcFields{rel: rel, pos: lit.Pos(), names: map[string]bool{}, unique: map[string]token.Pos{}}
		for _, el := range lit.Elts {
			fe, ok := el.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name, typed, unique := "", false, token.NoPos
			for _, kv := range fe.Elts {
				kv, ok := kv.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				k, _ := kv.Key.(*ast.Ident)
				if k == nil {
					continue
				}
				switch k.Name {
				case "Name":
					name, _ = stringLit(kv.Value)
				case "Type":
					if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == schemaAlias {
							typed = true
						}
					}
				case "Unique":
					if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
						unique = kv.Pos()
					}
				}
			}
			if name == "" || !typed {
				return true
			}
			name = strings.ToLower(name)
			fl.names[name] = true
			if unique.IsValid() {
				fl.unique[name] = unique
			}
		}
		pkg.fields = append(pkg.fields, fl)
		return false
	})
}

// dcSplitTop splits a column list on its top-level commas.
func dcSplitTop(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

// dcColumns reads a parenthesized column list's bare names; an
// expression (lower(email)) or a verb slot yields nothing for its item.
func dcColumns(list string) []string {
	var out []string
	for _, it := range strings.Split(list, ",") {
		f := strings.Fields(it)
		if len(f) == 0 {
			continue
		}
		c := strings.Trim(f[0], "\"`")
		if c == "" || strings.ContainsAny(c, "(%"+ecHole) {
			return nil
		}
		out = append(out, strings.ToLower(c))
	}
	return out
}

// dcAt names a position as file:line for a message.
func dcAt(p *contracts.Pass, pos token.Pos) string {
	at := p.Position(pos)
	return at.Filename + ":" + strconv.Itoa(at.Line)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
