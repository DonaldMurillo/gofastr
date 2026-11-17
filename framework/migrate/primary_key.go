package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// liveKey is a table's primary key columns in key order and, on Postgres,
// the constraint's name.
type liveKey struct {
	cols       []string
	constraint string
}

func sameKey(a, b []string) bool {
	return slices.EqualFunc(a, b, strings.EqualFold)
}

func keyHas(cols []string, col string) bool {
	return slices.ContainsFunc(cols, func(c string) bool { return strings.EqualFold(c, col) })
}

// defaultPKName is the name Postgres gives an unnamed primary-key
// constraint.
func defaultPKName(table string) string {
	t := strings.ToLower(tableBase(table))
	if len(t) > 58 {
		t = t[:58]
	}
	return t + "_pkey"
}

// readLiveKeysQ returns the primary key of each table that has one.
func readLiveKeysQ(ctx context.Context, q queryer, tables []string, dialect Dialect) (map[string]liveKey, error) {
	out := make(map[string]liveKey, len(tables))
	if len(tables) == 0 {
		return out, nil
	}
	if dialect == DialectPostgres {
		return readLiveKeysPostgres(ctx, q, tables)
	}
	for _, table := range tables {
		pragma, err := sqlitePragma("table_info", table)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, pragma)
		if err != nil {
			return nil, err
		}
		type keyCol struct {
			name string
			pos  int
		}
		var cols []keyCol
		for rows.Next() {
			var (
				cid, notnull, pk int
				name, typ        string
				dflt             any
			)
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return nil, err
			}
			if pk > 0 {
				cols = append(cols, keyCol{name, pk})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			continue
		}
		sort.Slice(cols, func(i, j int) bool { return cols[i].pos < cols[j].pos })
		k := liveKey{}
		for _, c := range cols {
			k.cols = append(k.cols, c.name)
		}
		out[table] = k
	}
	return out, nil
}

func readLiveKeysPostgres(ctx context.Context, q queryer, tables []string) (map[string]liveKey, error) {
	refs, args := pgTableRefs(tables)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.i, con.conname, a.attname
		FROM %s
		JOIN pg_namespace n ON n.nspname = coalesce(t.s, current_schema())
		JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = t.n
		JOIN pg_constraint con ON con.conrelid = c.oid AND con.contype = 'p'
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		ORDER BY t.i, k.ord
	`, refs), args...)
	if err != nil {
		return nil, fmt.Errorf("read primary keys: %w", err)
	}
	defer rows.Close()
	out := make(map[string]liveKey, len(tables))
	for rows.Next() {
		var i int
		var constraint, col string
		if err := rows.Scan(&i, &constraint, &col); err != nil {
			return nil, err
		}
		table := tables[i]
		k := out[table]
		k.constraint = constraint
		k.cols = append(k.cols, col)
		out[table] = k
	}
	return out, rows.Err()
}

func existingEntityTables(ordered []*entity.Entity, liveByTable map[string]map[string]string) []string {
	var out []string
	for _, ent := range ordered {
		if !ent.Config.Unmanaged && len(liveByTable[ent.GetTable()]) > 0 {
			out = append(out, ent.GetTable())
		}
	}
	return out
}

func keyChangeSummary(from, to []string) string {
	return fmt.Sprintf("change primary key (%s) → (%s)", strings.Join(from, ", "), strings.Join(to, ", "))
}

// postgresKeyChange renders the Postgres DDL that moves a table from its
// live key to the declared one; a nil tables map skips detaching the
// foreign keys onto it.
func postgresKeyChange(ent *entity.Entity, all map[string]*entity.Entity, qtable string, lk liveKey, tables map[string]map[string]string, extraKeep []string) (SchemaChange, error) {
	newKey := ent.KeyColumns()
	quoteAll := func(cols []string) (string, error) {
		safe := make([]string, len(cols))
		for i, c := range cols {
			q, err := query.SafeIdent(c)
			if err != nil {
				return "", fmt.Errorf("invalid key column %q: %w", c, err)
			}
			safe[i] = ddlIdent(q)
		}
		return strings.Join(safe, ", "), nil
	}
	tq := ddlIdent(qtable)
	keepsNotNull := func(col string) bool {
		f, ok := declaredField(ent, col)
		return ok && f.Required && f.AutoGenerate == schema.AutoNone
	}
	var up, down []string
	reversible := true
	var readd []string
	var deferred []SchemaChange
	if tables != nil && len(lk.cols) > 0 {
		in, err := incomingForeignKeys(ent, all, tables, extraKeep)
		if err != nil {
			return SchemaChange{}, err
		}
		if len(in.children) > 0 {
			up = append(up, detachForeignKeysSQL(qtable, in.children, in.keep))
			readd, deferred = in.readd, in.deferred
			reversible = false
		}
	}
	if len(lk.cols) > 0 {
		//gofastr:allow(quoteident) lk.constraint was read from pg_constraint with its case kept
		up = append(up, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tq, query.QuoteIdent(lk.constraint)))
	}
	for _, col := range lk.cols {
		if keyHas(newKey, col) {
			continue
		}
		if _, ok := declaredField(ent, col); !ok && !isFrameworkManagedColumn(strings.ToLower(col), ent) {
			reversible = false
		}
		if keepsNotNull(col) {
			continue
		}
		qcol, err := query.SafeIdent(col)
		if err != nil {
			return SchemaChange{}, fmt.Errorf("invalid column name %q: %w", col, err)
		}
		up = append(up, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tq, ddlIdent(qcol)))
	}
	if len(newKey) > 0 {
		cols, err := quoteAll(newKey)
		if err != nil {
			return SchemaChange{}, err
		}
		up = append(up, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", tq, cols))
		down = append(down, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tq, ddlIdent(defaultPKName(ent.GetTable()))))
		for _, col := range newKey {
			if keyHas(lk.cols, col) || keepsNotNull(col) {
				continue
			}
			qcol, err := query.SafeIdent(col)
			if err != nil {
				return SchemaChange{}, fmt.Errorf("invalid column name %q: %w", col, err)
			}
			down = append(down, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tq, ddlIdent(qcol)))
		}
	}
	if len(lk.cols) > 0 {
		cols, err := quoteAll(lk.cols)
		if err != nil {
			return SchemaChange{}, err
		}
		down = append(down, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", tq, cols))
	}
	up = append(up, readd...)
	downSQL := ""
	if reversible {
		downSQL = strings.Join(down, ";\n")
	}
	return SchemaChange{
		Summary:      fmt.Sprintf("%s: %s (destructive: fails if rows repeat the new key)", ent.GetName(), keyChangeSummary(lk.cols, newKey)),
		SQL:          strings.Join(up, ";\n"),
		Down:         downSQL,
		Destructive:  true,
		irreversible: !reversible,
		deferredFKs:  deferred,
	}, nil
}

func declaredField(ent *entity.Entity, col string) (schema.Field, bool) {
	for _, f := range ent.GetFields() {
		if strings.EqualFold(f.Name, col) {
			return f, true
		}
	}
	return schema.Field{}, false
}

// convergeKey brings an existing table's primary key in line with the
// entity's at boot; pre is the key read before the advisory lock.
func convergeKey(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, pre liveKey, deferred *[]SchemaChange) error {
	declared := ent.KeyColumns()
	if sameKey(uncutKey(ent, pre.cols, dialect), declared) {
		return nil
	}
	table := ent.GetTable()
	qtable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", table, err)
	}
	keys, err := readLiveKeysQ(ctx, exec, []string{table}, dialect)
	if err != nil {
		return fmt.Errorf("re-read primary key of %s: %w", table, err)
	}
	cur := keys[table]
	cur.cols = uncutKey(ent, cur.cols, dialect)
	if sameKey(cur.cols, declared) {
		return nil
	}
	rows, err := exec.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", ddlIdent(qtable)))
	if err != nil {
		return fmt.Errorf("count rows of %s: %w", table, err)
	}
	populated := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("count rows of %s: %w", table, err)
	}
	if populated {
		if len(cur.cols) == 0 {
			return fmt.Errorf("table %s has rows and no primary key, and the entity declares (%s): boot does not add a populated table's primary key; "+
				"generate a migration with `gofastr migrate generate` and review it, or apply DiffSchema's changes with ApplySchemaDiffWithOptions and AllowDestructive",
				table, strings.Join(declared, ", "))
		}
		return fmt.Errorf("table %s is keyed on (%s) but the entity declares (%s), and the table has rows: boot does not change a populated table's primary key; "+
			"generate a migration with `gofastr migrate generate` and review it, or apply DiffSchema's changes with ApplySchemaDiffWithOptions and AllowDestructive",
			table, strings.Join(cur.cols, ", "), strings.Join(declared, ", "))
	}
	var change SchemaChange
	if dialect == DialectPostgres {
		var referrers []string
		for _, name := range slices.Sorted(maps.Keys(all)) {
			other := all[name]
			if other.Config.Unmanaged {
				continue
			}
			fks, ferr := declaredForeignKeys(other, all)
			if ferr != nil {
				return ferr
			}
			if slices.ContainsFunc(fks, func(fk declaredFK) bool { return fk.target == ent }) {
				referrers = append(referrers, other.GetTable())
			}
		}
		pivots, perr := pivotTablesOnto(ent, all)
		if perr != nil {
			return perr
		}
		tables := map[string]map[string]string{}
		for _, t := range append(referrers, pivots...) {
			cols, rerr := readEntityColumnsQ(ctx, exec, t, dialect)
			if rerr != nil {
				return fmt.Errorf("re-read columns for %s: %w", t, rerr)
			}
			tables[t] = cols
		}
		change, err = postgresKeyChange(ent, all, qtable, cur, tables, nil)
	} else {
		stranded, serr := sqliteStrandedReferrers(ctx, exec, ent, cur.cols)
		if serr != nil {
			return serr
		}
		if len(stranded) > 0 {
			return strandedReferrersError(table, cur.cols, declared, stranded, "boot rebuilds no table but the one whose key changes")
		}
		var live map[string]string
		if live, err = readEntityColumnsQ(ctx, exec, table, dialect); err != nil {
			return fmt.Errorf("re-read columns for %s: %w", table, err)
		}
		change, err = sqliteRebuildChange(ent, all, dialect, live, nil, keyChangeSummary(cur.cols, declared), true)
	}
	if err != nil {
		return err
	}
	if dialect == DialectSQLite {
		if _, err := exec.ExecContext(ctx, "PRAGMA legacy_alter_table=ON"); err != nil {
			return fmt.Errorf("enable legacy_alter_table for the rebuild: %w", err)
		}
	}
	rerr := execRebuild(ctx, exec, change)
	if dialect == DialectSQLite {
		if _, err := exec.ExecContext(context.WithoutCancel(ctx), "PRAGMA legacy_alter_table=OFF"); err != nil && rerr == nil {
			return fmt.Errorf("restore legacy_alter_table after the rebuild: %w", err)
		}
	}
	if rerr != nil {
		return fmt.Errorf("%s: %w", change.Summary, rerr)
	}
	if deferred != nil {
		*deferred = append(*deferred, change.deferredFKs...)
	} else if len(change.deferredFKs) > 0 {
		return fmt.Errorf("%s: %s needs running after the other tables converge, and this caller runs none", change.Summary, change.deferredFKs[0].Summary)
	}
	return nil
}

func uniqueColumnSets(ent *entity.Entity) [][]string {
	sets := [][]string{ent.KeyColumns()}
	for _, f := range ent.Config.Fields {
		if f.Unique {
			sets = append(sets, []string{f.Name})
		}
	}
	for _, idx := range ent.Config.Indices {
		if idx.Unique && idx.Expression == "" && len(idx.Columns) > 0 {
			sets = append(sets, idx.Columns)
		}
	}
	return sets
}

// strandsReference reports whether a foreign key onto refCols of a table
// keyed on oldKey stops resolving once the table is rebuilt from ent.
func strandsReference(ent *entity.Entity, oldKey, refCols []string) bool {
	if len(refCols) == 0 {
		return len(oldKey) != len(ent.KeyColumns())
	}
	return !slices.ContainsFunc(uniqueColumnSets(ent), func(set []string) bool { return sameColumnSet(set, refCols) })
}

func sqliteStrandedReferrers(ctx context.Context, exec execQueryer, ent *entity.Entity, oldKey []string) ([]string, error) {
	table := ent.GetTable()
	rows, err := exec.QueryContext(ctx, `SELECT m.name, f.id, f."table", f."to"
		FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f
		WHERE m.type = 'table' AND lower(m.name) <> lower(?) AND lower(f."table") = lower(?)
		ORDER BY m.name, f.id, f.seq`, table, table)
	if err != nil {
		return nil, fmt.Errorf("read foreign keys onto %s: %w", table, err)
	}
	defer rows.Close()
	type fkRef struct {
		child string
		id    int
	}
	var order []fkRef
	refs := map[fkRef][]string{}
	for rows.Next() {
		var k fkRef
		var ref string
		var to sql.NullString
		if err := rows.Scan(&k.child, &k.id, &ref, &to); err != nil {
			return nil, fmt.Errorf("read foreign keys onto %s: %w", table, err)
		}
		if _, ok := refs[k]; !ok {
			order = append(order, k)
			refs[k] = nil
		}
		if to.Valid && to.String != "" {
			refs[k] = append(refs[k], to.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read foreign keys onto %s: %w", table, err)
	}
	var out []string
	for _, k := range order {
		if strandsReference(ent, oldKey, refs[k]) && !slices.Contains(out, k.child) {
			out = append(out, k.child)
		}
	}
	return out, nil
}

func strandedReferrersError(table string, oldKey, newKey, stranded []string, why string) error {
	return fmt.Errorf("table %s is keyed on (%s) but the entity declares (%s), and %s reference the old key: "+
		"SQLite cannot repoint a foreign key in place, and %s, so those tables would fail every later write with \"foreign key mismatch\"; "+
		"apply DiffSchema's changes with ApplySchemaDiffWithOptions and AllowDestructive, which rebuilds the entity tables that reference it "+
		"(not a generated many_to_many pivot), or write the migration by hand",
		table, strings.Join(oldKey, ", "), strings.Join(newKey, ", "), strings.Join(stranded, ", "), why)
}
