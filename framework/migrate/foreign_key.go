package migrate

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// liveFK is one foreign key as the database records it.
type liveFK struct {
	name     string
	cols     []string
	refTable string
	refCols  []string
	onDelete string

	refSchema  string
	refCurrent bool
}

// canonicalRefTables renames each live foreign key's columns, referenced
// table and referenced columns to the spelling the declarations use.
func canonicalRefTables(fks map[string][]liveFK, all map[string]*entity.Entity, dialect Dialect) {
	names := slices.Sorted(maps.Keys(all))
	for _, table := range slices.Sorted(maps.Keys(fks)) {
		list := fks[table]
		child := entityForTable(all, table)
		for i := range list {
			if child != nil {
				list[i].cols = uncutKey(child, list[i].cols, dialect)
			}
			for _, n := range names {
				if e := all[n]; !e.Config.Unmanaged && refIsTable(list[i], e.GetTable(), dialect) {
					list[i].refTable = e.GetTable()
					list[i].refCols = uncutKey(e, list[i].refCols, dialect)
					break
				}
			}
		}
	}
}

func refIsTable(fk liveFK, table string, dialect Dialect) bool {
	schema, name := splitTable(table)
	if dialect != DialectPostgres {
		return strings.EqualFold(tableBase(fk.refTable), name)
	}
	if !strings.EqualFold(tableBase(fk.refTable), pgIdentCut(name)) {
		return false
	}
	if schema == "" {
		return fk.refCurrent
	}
	return strings.EqualFold(pgIdentCut(schema), fk.refSchema)
}

func canonicalOnDelete(a string) string {
	a = strings.ToUpper(strings.TrimSpace(a))
	if a == "" {
		return string(entity.OnDeleteNoAction)
	}
	return a
}

// sameForeignKey reports whether a live and a declared foreign key have the
// same referenced table, column pairs in any order, and ON DELETE action.
func sameForeignKey(l liveFK, d declaredFK) bool {
	if !strings.EqualFold(l.refTable, d.refTable) || len(l.cols) != len(d.cols) || len(l.refCols) != len(l.cols) {
		return false
	}
	if canonicalOnDelete(l.onDelete) != canonicalOnDelete(string(d.onDelete)) {
		return false
	}
	type pair struct{ col, ref string }
	pairs := make(map[pair]bool, len(l.cols))
	for i := range l.cols {
		pairs[pair{strings.ToLower(l.cols[i]), strings.ToLower(l.refCols[i])}] = true
	}
	for i := range d.cols {
		if !pairs[pair{strings.ToLower(d.cols[i]), strings.ToLower(d.refCols[i])}] {
			return false
		}
	}
	return true
}

func describeFK(cols []string, refTable string, refCols []string) string {
	return fmt.Sprintf("(%s) → %s(%s)", strings.Join(cols, ", "), refTable, strings.Join(refCols, ", "))
}

// readLiveForeignKeysQ returns the foreign keys each table holds.
func readLiveForeignKeysQ(ctx context.Context, q queryer, tables []string, dialect Dialect) (map[string][]liveFK, error) {
	out := make(map[string][]liveFK, len(tables))
	if len(tables) == 0 {
		return out, nil
	}
	if dialect == DialectPostgres {
		return readLiveForeignKeysPostgres(ctx, q, tables)
	}
	for _, table := range tables {
		pragma, err := sqlitePragma("foreign_key_list", table)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, pragma)
		if err != nil {
			return nil, fmt.Errorf("read foreign keys of %s: %w", table, err)
		}
		byID := map[int]*liveFK{}
		var ids []int
		for rows.Next() {
			var (
				id, seq                         int
				ref, from                       string
				to                              *string
				onUpdate, onDelete, matchClause string
			)
			if err := rows.Scan(&id, &seq, &ref, &from, &to, &onUpdate, &onDelete, &matchClause); err != nil {
				rows.Close()
				return nil, err
			}
			fk, ok := byID[id]
			if !ok {
				fk = &liveFK{refTable: ref, onDelete: canonicalOnDelete(onDelete)}
				byID[id] = fk
				ids = append(ids, id)
			}
			fk.cols = append(fk.cols, from)
			if to != nil {
				fk.refCols = append(fk.refCols, *to)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		slices.Sort(ids)
		for _, id := range ids {
			fk := *byID[id]
			if len(fk.refCols) == 0 {
				keys, err := readLiveKeysQ(ctx, q, []string{fk.refTable}, dialect)
				if err != nil {
					return nil, fmt.Errorf("read the key %s.%v references: %w", table, fk.cols, err)
				}
				fk.refCols = keys[fk.refTable].cols
			}
			out[table] = append(out[table], fk)
		}
	}
	return out, nil
}

func readLiveForeignKeysPostgres(ctx context.Context, q queryer, tables []string) (map[string][]liveFK, error) {
	refs, args := pgTableRefs(tables)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.i, con.conname, rn.nspname, rn.nspname = current_schema(), rc.relname,
			a.attname, ra.attname, con.confdeltype
		FROM %s
		JOIN pg_namespace n ON n.nspname = coalesce(t.s, current_schema())
		JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = t.n
		JOIN pg_constraint con ON con.conrelid = c.oid AND con.contype = 'f'
		JOIN pg_class rc ON rc.oid = con.confrelid
		JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(attnum, refattnum, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = k.refattnum
		ORDER BY t.i, con.conname, k.ord
	`, refs), args...)
	if err != nil {
		return nil, fmt.Errorf("read foreign keys: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]liveFK, len(tables))
	for rows.Next() {
		var i int
		var current bool
		var name, refSchema, ref, col, refCol, action string
		if err := rows.Scan(&i, &name, &refSchema, &current, &ref, &col, &refCol, &action); err != nil {
			return nil, err
		}
		table := tables[i]
		fks := out[table]
		if n := len(fks); n == 0 || fks[n-1].name != name {
			refTable := ref
			if !current {
				refTable = refSchema + "." + ref
			}
			fks = append(fks, liveFK{name: name, refTable: refTable, refSchema: refSchema, refCurrent: current, onDelete: postgresDeleteAction(action)})
		}
		last := &fks[len(fks)-1]
		last.cols = append(last.cols, col)
		last.refCols = append(last.refCols, refCol)
		out[table] = fks
	}
	return out, rows.Err()
}

func postgresDeleteAction(code string) string {
	switch code {
	case "r":
		return string(entity.OnDeleteRestrict)
	case "c":
		return string(entity.OnDeleteCascade)
	case "n":
		return string(entity.OnDeleteSetNull)
	case "d":
		return "SET DEFAULT"
	}
	return string(entity.OnDeleteNoAction)
}

// fkDiffEnv carries what the foreign-key comparison needs beyond the one
// table.
type fkDiffEnv struct {
	fks             map[string][]liveFK
	tables          map[string]map[string]string
	keyChanged      map[string][]string
	deferred        []SchemaChange
	deferredNotNull map[string][]string
	uniques         map[string][]string
	staleUniques    map[string][]string
	tableDDL        map[string]string
	indexed         map[string]map[string]bool
}

// renamesApplied maps each column a rename hint renames (lower case) to its
// new name.
func renamesApplied(ent *entity.Entity, live map[string]string) map[string]string {
	out := map[string]string{}
	for _, oldName := range slices.Sorted(maps.Keys(ent.Config.Renames)) {
		newName := ent.Config.Renames[oldName]
		if !hasColumns(live, []string{oldName}) {
			continue
		}
		if _, ok := declaredField(ent, newName); !ok {
			continue
		}
		out[strings.ToLower(oldName)] = newName
	}
	return out
}

func entityForTable(all map[string]*entity.Entity, table string) *entity.Entity {
	for _, name := range slices.Sorted(maps.Keys(all)) {
		if e := all[name]; !e.Config.Unmanaged && strings.EqualFold(e.GetTable(), table) {
			return e
		}
	}
	return nil
}

// foreignKeyChanges compares an existing table's live foreign keys with the
// declared ones: constraint changes on Postgres, only whether they differ on
// SQLite.
func foreignKeyChanges(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, qtable string, env *fkDiffEnv, renameNewByOld map[string]string) (changes []SchemaChange, differ bool, err error) {
	declared, err := declaredForeignKeys(ent, all)
	if err != nil {
		return nil, false, err
	}
	var live []liveFK
	for _, fk := range lookupFKsCase(env.fks, ent.GetTable()) {
		fk.cols = slices.Clone(fk.cols)
		for i, c := range fk.cols {
			if n, ok := renameNewByOld[strings.ToLower(c)]; ok {
				fk.cols[i] = n
			}
		}
		if ref := entityForTable(all, fk.refTable); ref != nil {
			refRenames := renamesApplied(ref, lookupTableCols(env.tables, ref.GetTable()))
			fk.refCols = slices.Clone(fk.refCols)
			for i, c := range fk.refCols {
				if n, ok := refRenames[strings.ToLower(c)]; ok {
					fk.refCols[i] = n
				}
			}
		}
		if old, ok := env.keyChanged[strings.ToLower(fk.refTable)]; ok && sameColumnSet(fk.refCols, old) {
			if slices.ContainsFunc(declared, func(d declaredFK) bool {
				return strings.EqualFold(d.refTable, fk.refTable) && sameColumnSet(d.cols, fk.cols)
			}) {
				continue
			}
		}
		live = append(live, fk)
	}
	for _, d := range declared {
		if _, ok := env.keyChanged[strings.ToLower(d.refTable)]; ok {
			live = append(live, liveFK{cols: d.cols, refTable: d.refTable, refCols: d.refCols, onDelete: canonicalOnDelete(string(d.onDelete))})
		}
	}

	var missing []declaredFK
	for _, d := range declared {
		if !slices.ContainsFunc(live, func(l liveFK) bool { return sameForeignKey(l, d) }) {
			missing = append(missing, d)
		}
	}
	var stale []liveFK
	for _, l := range live {
		if !slices.ContainsFunc(declared, func(d declaredFK) bool { return sameForeignKey(l, d) }) {
			stale = append(stale, l)
		}
	}
	if len(missing) == 0 && len(stale) == 0 {
		return nil, false, nil
	}
	if dialect != DialectPostgres {
		return nil, true, nil
	}
	replaced := map[int]bool{}
	for _, d := range missing {
		var drops []string
		var from []string
		for i, l := range stale {
			if replaced[i] || !strings.EqualFold(l.refTable, d.refTable) || !sameColumnSet(l.cols, d.cols) {
				continue
			}
			replaced[i] = true
			//gofastr:allow(quoteident) l.name was read from pg_constraint with its case kept
			drops = append(drops, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", ddlIdent(qtable), query.QuoteIdent(l.name)))
			from = append(from, describeFK(l.cols, l.refTable, l.refCols))
		}
		stmts := append(drops, fmt.Sprintf("ALTER TABLE %s ADD %s", ddlIdent(qtable), d.clause(DialectPostgres)))
		summary := fmt.Sprintf("%s: add foreign key %s", ent.GetName(), describeFK(d.cols, d.refTable, d.refCols))
		if len(from) > 0 {
			summary = fmt.Sprintf("%s: change foreign key %s → %s", ent.GetName(), strings.Join(from, ", "), describeFK(d.cols, d.refTable, d.refCols))
		}
		if d.onDelete != "" {
			summary += " ON DELETE " + string(d.onDelete)
		}
		changes = append(changes, SchemaChange{
			Summary:      summary + " (fails if existing rows violate it)",
			SQL:          strings.Join(stmts, ";\n"),
			irreversible: len(from) > 0,
		})
	}
	for i, l := range stale {
		if replaced[i] {
			continue
		}
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("%s: drop foreign key %s (destructive: no relation declares it)", ent.GetName(), describeFK(l.cols, l.refTable, l.refCols)),
			//gofastr:allow(quoteident) l.name was read from pg_constraint with its case kept
			SQL:          fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", ddlIdent(qtable), query.QuoteIdent(l.name)),
			Destructive:  true,
			irreversible: true,
		})
	}
	return changes, true, nil
}

func hasColumns(set map[string]string, cols []string) bool {
	for _, c := range cols {
		found := false
		for k := range set {
			if strings.EqualFold(k, c) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sameColumnSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, c := range a {
		if !keyHas(b, c) {
			return false
		}
	}
	return true
}

func lookupFKsCase(m map[string][]liveFK, table string) []liveFK {
	if fks, ok := m[table]; ok {
		return fks
	}
	for k, fks := range m {
		if strings.EqualFold(k, table) {
			return fks
		}
	}
	return nil
}

// incomingFKs is what a Postgres key change needs about the foreign keys
// onto its table.
type incomingFKs struct {
	children []string
	keep     []string
	readd    []string
	deferred []SchemaChange
}

// fkKeepKey identifies a foreign key by its child table and sorted
// lower-case column set.
func fkKeepKey(table string, cols []string) string {
	low := make([]string, len(cols))
	for i, c := range cols {
		low[i] = strings.ToLower(strings.Trim(strings.TrimSpace(c), `"`))
	}
	slices.Sort(low)
	return strings.ToLower(table) + ":" + strings.Join(low, ",")
}

var reFKReferenceCols = regexp.MustCompile(`(?i)\bREFERENCES\s+("?[a-zA-Z0-9_]+"?(?:\."?[a-zA-Z0-9_]+"?)*)\s*(?:\(([^)]*)\))?`)

// recordedReferencesOnto lists, per table, the column lists of the foreign
// keys onto table in the snapshot's CREATE TABLEs; an empty list references
// the key.
func recordedReferencesOnto(dialect Dialect, tableDDL map[string]string, table string) map[string][][]string {
	out := map[string][][]string{}
	for _, name := range slices.Sorted(maps.Keys(tableDDL)) {
		if strings.EqualFold(name, table) {
			continue
		}
		for _, m := range reFKReferenceCols.FindAllStringSubmatch(ddlWithoutLiterals(tableDDL[name]), -1) {
			if !refersTo(dialect, name, unquoteDDLIdent(m[1]), table) {
				continue
			}
			var cols []string
			for _, c := range strings.Split(m[2], ",") {
				if c = strings.Trim(strings.TrimSpace(c), `"`); c != "" {
					cols = append(cols, c)
				}
			}
			out[name] = append(out[name], cols)
		}
	}
	return out
}

// incomingForeignKeys lists the declared foreign keys onto ent held by the
// tables in tables, entity tables and generated pivots.
func incomingForeignKeys(ent *entity.Entity, all map[string]*entity.Entity, tables map[string]map[string]string, extraKeep []string) (incomingFKs, error) {
	var in incomingFKs
	in.keep = append(in.keep, extraKeep...)
	add := func(holder, table string, cols map[string]string, oldNames map[string]string, fks []declaredFK) error {
		qchild, err := query.SafeIdent(table)
		if err != nil {
			return fmt.Errorf("invalid table name %q: %w", table, err)
		}
		holds := false
		for _, fk := range fks {
			if fk.target != ent {
				continue
			}
			holds = true
			old := make([]string, len(fk.cols))
			for i, c := range fk.cols {
				old[i] = c
				if o, ok := oldNames[strings.ToLower(c)]; ok {
					old[i] = o
				}
			}
			in.keep = append(in.keep, fkKeepKey(table, old))
			stmt := fmt.Sprintf("ALTER TABLE %s ADD %s", ddlIdent(qchild), fk.clause(DialectPostgres))
			if hasColumns(cols, fk.cols) {
				in.readd = append(in.readd, stmt)
				continue
			}
			in.deferred = append(in.deferred, SchemaChange{
				Summary: fmt.Sprintf("%s: add foreign key %s after the key change of %s (fails if existing rows violate it)",
					holder, describeFK(fk.cols, fk.refTable, fk.refCols), ent.GetTable()),
				SQL: stmt,
			})
		}
		if holds && !slices.Contains(in.children, qchild) {
			in.children = append(in.children, qchild)
		}
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(all)) {
		other := all[name]
		if other.Config.Unmanaged {
			continue
		}
		cols := lookupTableCols(tables, other.GetTable())
		if len(cols) == 0 {
			continue
		}
		fks, err := declaredForeignKeys(other, all)
		if err != nil {
			return incomingFKs{}, err
		}
		oldNames := map[string]string{}
		for o, n := range renamesApplied(other, cols) {
			oldNames[strings.ToLower(n)] = o
		}
		if err := add(other.GetName(), other.GetTable(), cols, oldNames, fks); err != nil {
			return incomingFKs{}, err
		}
	}
	ordered, err := topoSortEntities(all)
	if err != nil {
		return incomingFKs{}, err
	}
	for owner, rel := range generatedPivots(ordered, all) {
		cols := lookupTableCols(tables, rel.Through)
		if len(cols) == 0 {
			continue
		}
		spec, err := buildPivotSpec(owner, rel, all, DialectPostgres)
		if err != nil {
			return incomingFKs{}, err
		}
		var fks []declaredFK
		for _, fk := range spec.fks {
			if hasColumns(cols, fk.cols) {
				fks = append(fks, fk)
			}
		}
		if err := add(rel.Through, rel.Through, cols, nil, fks); err != nil {
			return incomingFKs{}, err
		}
	}
	return in, nil
}

func pivotTablesOnto(ent *entity.Entity, all map[string]*entity.Entity) ([]string, error) {
	ordered, err := topoSortEntities(all)
	if err != nil {
		return nil, err
	}
	var out []string
	for owner, rel := range generatedPivots(ordered, all) {
		if owner == ent || all[rel.Entity] == ent {
			out = append(out, rel.Through)
		}
	}
	return out, nil
}

// detachForeignKeysSQL renders a Postgres block that drops the foreign keys
// children hold onto table's primary key and fails on one not in keep.
func detachForeignKeysSQL(qtable string, children, keep []string) string {
	regs := make([]string, len(children))
	for i, c := range children {
		regs[i] = "to_regclass(" + quoteSQLLiteral(c) + ")"
	}
	keeps := make([]string, len(keep))
	for i, k := range keep {
		keeps[i] = quoteSQLLiteral(k)
	}
	return fmt.Sprintf(`DO $$
DECLARE r record;
BEGIN
	FOR r IN SELECT con.conrelid::regclass AS child, c.relname, n.nspname, con.conname,
			(SELECT string_agg(lower(a.attname), ',' ORDER BY lower(a.attname)) FROM unnest(con.conkey) AS k(n)
				JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n) AS cols
		FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE con.contype = 'f' AND con.confrelid = %[1]s::regclass AND con.conrelid IN (%[2]s)
			AND con.conindid = (SELECT pk.conindid FROM pg_constraint pk WHERE pk.conrelid = %[1]s::regclass AND pk.contype = 'p')
	LOOP
		IF NOT (lower(r.nspname) || '.' || lower(r.relname) || ':' || r.cols = ANY (ARRAY[%[3]s]::text[])
				OR (r.nspname = current_schema() AND lower(r.relname) || ':' || r.cols = ANY (ARRAY[%[3]s]::text[]))) THEN
			RAISE EXCEPTION 'foreign key %% on %% depends on the primary key of %% and no relation declares it: the key change does not drop it; drop it first, or apply DiffSchema''s changes with AllowDestructive', r.conname, r.child, %[1]s;
		END IF;
		EXECUTE format('ALTER TABLE %%s DROP CONSTRAINT %%I', r.child, r.conname);
	END LOOP;
END $$`, quoteSQLLiteral(qtable), strings.Join(regs, ", "), strings.Join(keeps, ", "))
}
