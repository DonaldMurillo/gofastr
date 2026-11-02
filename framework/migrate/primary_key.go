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

// Primary-key changes
//
// An entity's key columns (EntityConfig.PrimaryKey) can change after its
// table exists: a join table that started with an "id" column moves to the
// pair it links. The diff compares the declared key against the live (or
// snapshot) key and emits the change as Destructive: Postgres drops the
// key constraint and adds the new one, and SQLite, which cannot alter a
// key in place, rebuilds the table. Boot applies the change only to an
// empty table; a table with rows is refused until a reviewed migration or
// ApplySchemaDiffWithOptions with AllowDestructive makes the change.

// liveKey is a table's primary key as the database (or the snapshot)
// records it: the key columns in key order, and on Postgres the
// constraint's name. A table with no primary key has no cols.
type liveKey struct {
	cols       []string
	constraint string
}

// sameKey reports whether two key column lists name the same columns in
// the same order. Postgres folds unquoted identifiers, so the comparison
// ignores case.
func sameKey(a, b []string) bool {
	return slices.EqualFunc(a, b, strings.EqualFold)
}

// keyHas reports whether a key column list names col, ignoring case.
func keyHas(cols []string, col string) bool {
	return slices.ContainsFunc(cols, func(c string) bool { return strings.EqualFold(c, col) })
}

// defaultPKName is the name Postgres gives an unnamed primary-key
// constraint: the table's own folded name (without its schema) cut to 58
// bytes, then "_pkey". The snapshot path, which never sees the live
// constraint, drops it by this name; SafeIdent has already limited the
// table name to ASCII.
func defaultPKName(table string) string {
	t := strings.ToLower(tableBase(table))
	if len(t) > 58 {
		t = t[:58]
	}
	return t + "_pkey"
}

// readLiveKeysQ returns the primary key of each table that has one. A table
// that exists but is missing from the result has no primary key.
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

// existingEntityTables lists the managed entities' tables that exist live,
// the tables whose primary key the diff and boot compare.
func existingEntityTables(ordered []*entity.Entity, liveByTable map[string]map[string]string) []string {
	var out []string
	for _, ent := range ordered {
		if !ent.Config.Unmanaged && len(liveByTable[ent.GetTable()]) > 0 {
			out = append(out, ent.GetTable())
		}
	}
	return out
}

// keyChangeSummary describes a key change for a SchemaChange summary.
func keyChangeSummary(from, to []string) string {
	return fmt.Sprintf("change primary key (%s) → (%s)", strings.Join(from, ", "), strings.Join(to, ", "))
}

// postgresKeyChange renders the Postgres DDL that moves a table from its
// live key to the declared one: drop the old constraint, clear the NOT NULL
// the old key gave a column that stays in the table but leaves the key
// (unless the field is Required), and add the new key. ADD PRIMARY KEY sets
// NOT NULL on the new key columns itself, and fails on a table whose rows
// repeat a key or leave a key column NULL.
//
// Postgres refuses to drop a key constraint another table's foreign key
// depends on, so with tables (the live or snapshot column sets) the change
// first detaches the foreign keys the entity's declared referrers and its
// generated pivots hold onto the key, and re-adds each declared one, now
// naming the new key, once the key is back. A declared key whose columns the
// child gains only in its own changes (a rename, an added column) cannot be
// re-added here; it rides in deferredFKs, which the caller runs after every
// table's changes. The detach drops only the keys a relation declares and
// those in extraKeep (fkKeepKey values the caller reported as destructive
// drops of their own); any other key onto the old key fails the change. A
// nil tables map skips all that (boot's additive pass, which filters the
// change out anyway).
//
// The Down mirrors it: drop the new key, clear the NOT NULL the new key gave
// a column outside the old one, and add the old key back. It is empty when
// an old key column leaves the entity, because the diff drops that column
// and its values, and no key can be restored on the empty column a Down
// re-adds, and when foreign keys were detached, whose old definitions
// neither the live nor the snapshot column set records.
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
	// The statements name the table and its columns as the DDL does: a
	// keyword quoted, anything else bare. qtable stays the plain name for
	// the detach block and the catalog reads.
	tq := ddlIdent(qtable)
	// keepsNotNull reports whether a column outside a key stays NOT NULL on
	// its own: a Required field with no generated value.
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
		// An undeclared column is dropped by the diff's DROP COLUMN, but
		// boot keeps it, and a NOT NULL left from the key with no default
		// would refuse every insert.
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

// declaredField finds the entity field named col, ignoring case.
func declaredField(ent *entity.Entity, col string) (schema.Field, bool) {
	for _, f := range ent.GetFields() {
		if strings.EqualFold(f.Name, col) {
			return f, true
		}
	}
	return schema.Field{}, false
}

// convergeKey brings an existing table's primary key in line with the
// entity's at boot. pre is the key read before the advisory lock; the key is
// read again on the lock-holding connection before anything changes, so a
// replica that lost the boot race sees its peer's change and does nothing.
//
// An empty table gets the new key. A table with rows is refused: rows
// keyed one way do not satisfy another, and with no key at all nothing
// holds the declared one unique, so a create or update by id could touch
// several rows.
//
// On Postgres a declared foreign key onto the table whose child gains its
// columns only later in the boot (a column the child adds) is appended to
// deferred, for the caller to run once every table has converged.
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
		// The tables holding a declared foreign key onto this one, entity
		// tables and generated pivots, read on the lock-holding connection,
		// so the change can detach and re-add those keys around the key swap.
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
		// Boot drops no foreign key a relation does not declare: no
		// extraKeep, so the detach refuses one.
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
		// The rebuild ends in a RENAME, which modern SQLite runs only
		// after re-parsing every view and trigger in the schema; a view
		// that reads this table names a table the rebuild just dropped,
		// so the RENAME fails. Legacy mode skips that check, as the live
		// path (withSQLiteRebuildConn) and the generated file already do.
		// The pragma belongs to the connection, not the transaction, so
		// it goes back off whether or not the rebuild worked.
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

// uniqueColumnSets lists the column sets the entity's table holds unique
// once built from its declaration: its key, each Unique field, and each
// unique column index. A SQLite foreign key must reference one of them.
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

// strandsReference reports whether a foreign key referencing refCols of a
// table keyed on oldKey stops resolving once the table is rebuilt from
// ent's declaration. An empty refCols references the key, whatever its
// columns, so only a change in its width strands it.
func strandsReference(ent *entity.Entity, oldKey, refCols []string) bool {
	if len(refCols) == 0 {
		return len(oldKey) != len(ent.KeyColumns())
	}
	return !slices.ContainsFunc(uniqueColumnSets(ent), func(set []string) bool { return sameColumnSet(set, refCols) })
}

// sqliteStrandedReferrers lists the other tables whose live foreign keys
// onto ent's table would no longer resolve once a key change rebuilds it:
// SQLite names a foreign key's referenced columns, so a key that moves
// leaves them pointing at columns no key or unique index holds, and every
// later write to those tables fails with "foreign key mismatch".
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

// strandedReferrersError refuses a SQLite key change that would strand the
// foreign keys of the named tables.
func strandedReferrersError(table string, oldKey, newKey, stranded []string, why string) error {
	return fmt.Errorf("table %s is keyed on (%s) but the entity declares (%s), and %s reference the old key: "+
		"SQLite cannot repoint a foreign key in place, and %s, so those tables would fail every later write with \"foreign key mismatch\"; "+
		"apply DiffSchema's changes with ApplySchemaDiffWithOptions and AllowDestructive, which rebuilds the entity tables that reference it "+
		"(not a generated many_to_many pivot), or write the migration by hand",
		table, strings.Join(oldKey, ", "), strings.Join(newKey, ", "), strings.Join(stranded, ", "), why)
}
