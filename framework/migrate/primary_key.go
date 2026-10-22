package migrate

import (
	"context"
	"fmt"
	"log/slog"
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
// constraint: the folded table name cut to 58 bytes, then "_pkey". The
// snapshot path, which never sees the live constraint, drops it by this
// name; SafeIdent has already limited the table name to ASCII.
func defaultPKName(table string) string {
	t := strings.ToLower(table)
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
		if _, err := query.SafeIdent(table); err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
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
	placeholders := make([]string, len(tables))
	args := make([]any, len(tables))
	byLower := make(map[string]string, len(tables))
	for i, t := range tables {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = strings.ToLower(t)
		byLower[strings.ToLower(t)] = t
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.relname, con.conname, a.attname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		WHERE con.contype = 'p' AND n.nspname = current_schema() AND lower(c.relname) IN (%s)
		ORDER BY c.relname, k.ord
	`, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, fmt.Errorf("read primary keys: %w", err)
	}
	defer rows.Close()
	out := make(map[string]liveKey, len(tables))
	for rows.Next() {
		var table, constraint, col string
		if err := rows.Scan(&table, &constraint, &col); err != nil {
			return nil, err
		}
		if orig, ok := byLower[strings.ToLower(table)]; ok {
			table = orig
		}
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
// The Down mirrors it: drop the new key, clear the NOT NULL the new key gave
// a column outside the old one, and add the old key back. It is empty when
// an old key column leaves the entity, because the diff drops that column
// and its values, and no key can be restored on the empty column a Down
// re-adds.
func postgresKeyChange(ent *entity.Entity, qtable string, lk liveKey) (SchemaChange, error) {
	newKey := ent.KeyColumns()
	quoteAll := func(cols []string) (string, error) {
		safe := make([]string, len(cols))
		for i, c := range cols {
			q, err := query.SafeIdent(c)
			if err != nil {
				return "", fmt.Errorf("invalid key column %q: %w", c, err)
			}
			safe[i] = q
		}
		return strings.Join(safe, ", "), nil
	}
	// keepsNotNull reports whether a column outside a key stays NOT NULL on
	// its own: a Required field with no generated value.
	keepsNotNull := func(col string) bool {
		f, ok := declaredField(ent, col)
		return ok && f.Required && f.AutoGenerate == schema.AutoNone
	}
	var up, down []string
	reversible := true
	if len(lk.cols) > 0 {
		up = append(up, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", qtable, query.QuoteIdent(lk.constraint)))
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
		up = append(up, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", qtable, qcol))
	}
	if len(newKey) > 0 {
		cols, err := quoteAll(newKey)
		if err != nil {
			return SchemaChange{}, err
		}
		up = append(up, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", qtable, cols))
		down = append(down, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", qtable, defaultPKName(ent.GetTable())))
		for _, col := range newKey {
			if keyHas(lk.cols, col) || keepsNotNull(col) {
				continue
			}
			qcol, err := query.SafeIdent(col)
			if err != nil {
				return SchemaChange{}, fmt.Errorf("invalid column name %q: %w", col, err)
			}
			down = append(down, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", qtable, qcol))
		}
	}
	if len(lk.cols) > 0 {
		cols, err := quoteAll(lk.cols)
		if err != nil {
			return SchemaChange{}, err
		}
		down = append(down, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", qtable, cols))
	}
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
// An empty table gets the new key. A table with rows is refused when it
// already has a different key: rows keyed one way do not satisfy another,
// and a create or update on the wrong key could touch several rows. A table
// with rows and no key at all is left as it is with a warning, because
// nothing in it contradicts the declared key.
func convergeKey(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, pre liveKey) error {
	declared := ent.KeyColumns()
	if sameKey(pre.cols, declared) {
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
	if sameKey(cur.cols, declared) {
		return nil
	}
	rows, err := exec.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", qtable))
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
			slog.Warn("migrate: table has rows and no primary key; the entity's key is not enforced by the database",
				"table", table, "key", strings.Join(declared, ","),
				"fix", "generate a migration with `gofastr migrate generate` and review it")
			return nil
		}
		return fmt.Errorf("table %s is keyed on (%s) but the entity declares (%s), and the table has rows: boot does not change a populated table's primary key; "+
			"generate a migration with `gofastr migrate generate` and review it, or apply DiffSchema's changes with ApplySchemaDiffWithOptions and AllowDestructive",
			table, strings.Join(cur.cols, ", "), strings.Join(declared, ", "))
	}
	var change SchemaChange
	if dialect == DialectPostgres {
		change, err = postgresKeyChange(ent, qtable, cur)
	} else {
		var live map[string]string
		if live, err = readLiveColumnsQ(ctx, exec, table, dialect); err != nil {
			return fmt.Errorf("re-read columns for %s: %w", table, err)
		}
		change, err = sqliteRebuildChange(ent, all, dialect, live, nil, keyChangeSummary(cur.cols, declared), true)
	}
	if err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, change.SQL); err != nil {
		return fmt.Errorf("%s: %w", change.Summary, err)
	}
	return nil
}
