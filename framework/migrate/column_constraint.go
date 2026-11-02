package migrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// NOT NULL and DEFAULT on an existing column
//
// CREATE TABLE and ADD COLUMN write a column's NOT NULL and DEFAULT, and
// nothing compared them after that: the snapshot's column sets hold types
// only. The CREATE TABLE the snapshot records holds both, so generate
// reads each column's previous NOT NULL and DEFAULT from it and compares
// them with the declared ones. On Postgres a change is an ALTER COLUMN;
// SQLite has no ALTER COLUMN, so a change there joins the table rebuild.
// Boot compares neither: a NOT NULL a hand-written migration put on a
// column the entity leaves optional is the host's, and boot would remove
// it.

// colConstraint is the part of a column definition after its type that
// the diff compares: whether it is NOT NULL, and its " DEFAULT …" clause
// ("" for none).
type colConstraint struct {
	notNull bool
	dflt    string
}

// declaredConstraint is f's NOT NULL and DEFAULT as columnDefs writes
// them, given the entity's key columns.
func declaredConstraint(f schema.Field, keys []string, dialect Dialect) colConstraint {
	inKey := slices.Contains(keys, f.Name)
	return colConstraint{
		notNull: (f.Required && f.AutoGenerate == schema.AutoNone) || (inKey && len(keys) > 1),
		dflt:    ColumnDefaultClause(f, dialect),
	}
}

// recordedConstraints reads the NOT NULL and DEFAULT of each column a
// buildCreateTableSQL statement defines, keyed by the lower-cased column
// name. The definitions are joined with ",\n\t", which a default's
// literal can hold too, so the split skips quoted text. The DEFAULT
// clause is the last thing columnDefs writes, so it runs to the end of
// the definition.
func recordedConstraints(ddl string) (map[string]colConstraint, bool) {
	_, body, ok := strings.Cut(ddl, " (\n\t")
	if !ok {
		return nil, false
	}
	body = strings.TrimSuffix(body, "\n)")
	out := map[string]colConstraint{}
	for _, def := range splitColumnDefs(body) {
		name, rest, _ := strings.Cut(def, " ")
		head, dflt, hasDefault := strings.Cut(rest, " DEFAULT ")
		c := colConstraint{notNull: strings.Contains(" "+head+" ", " NOT NULL ")}
		if hasDefault {
			c.dflt = " DEFAULT " + dflt
		}
		out[strings.ToLower(unquoteDDLIdent(name))] = c
	}
	return out, true
}

// splitColumnDefs splits a CREATE TABLE body on the ",\n\t" between its
// definitions, outside quoted text.
func splitColumnDefs(body string) []string {
	var out []string
	start := 0
	for i := 0; i < len(body); {
		if j := skipInert(body, i); j > i {
			i = j
			continue
		}
		if strings.HasPrefix(body[i:], ",\n\t") {
			out = append(out, body[start:i])
			i += len(",\n\t")
			start = i
			continue
		}
		i++
	}
	return append(out, body[start:])
}

// constraintChanges compares each existing column's declared NOT NULL and
// DEFAULT with prevDDL's (the snapshot's CREATE TABLE for ent's table). A
// column prevDDL does not define, such as one added in this change, is
// not compared; a key column is the key diff's, and a renamed one is
// judged by its old name. defaultOwned names
// the columns whose Postgres retype already sets the declared default.
// On Postgres it returns one ALTER per change; on SQLite it returns only
// the changed columns, which the caller turns into a table rebuild.
func constraintChanges(ent *entity.Entity, dialect Dialect, qtable, prevDDL string, renameOldByNew map[string]string, defaultOwned map[string]bool) (alters []SchemaChange, changed []string, err error) {
	prev, ok := recordedConstraints(prevDDL)
	if !ok {
		return nil, nil, nil
	}
	keys := ent.KeyColumns()
	for _, f := range ent.GetFields() {
		if slices.Contains(keys, f.Name) {
			continue
		}
		src := strings.ToLower(f.Name)
		if old, ok := renameOldByNew[src]; ok {
			src = old
		}
		was, ok := prev[src]
		if !ok {
			continue
		}
		now := declaredConstraint(f, keys, dialect)
		nullFlip := now.notNull != was.notNull
		dfltFlip := now.dflt != was.dflt && !defaultOwned[strings.ToLower(f.Name)]
		if !nullFlip && !dfltFlip {
			continue
		}
		changed = append(changed, f.Name)
		if dialect == DialectSQLite {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		if nullFlip {
			set, unset := "SET NOT NULL", "DROP NOT NULL"
			summary := fmt.Sprintf("%s: set %s NOT NULL (fails while a row holds NULL)", ent.GetName(), f.Name)
			if !now.notNull {
				set, unset = unset, set
				summary = fmt.Sprintf("%s: drop NOT NULL on %s", ent.GetName(), f.Name)
			}
			alters = append(alters, SchemaChange{
				Summary: summary,
				SQL:     fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", ddlIdent(qtable), ddlIdent(qcol), set),
				Down:    fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", ddlIdent(qtable), ddlIdent(qcol), unset),
			})
		}
		if dfltFlip {
			summary := fmt.Sprintf("%s: set %s%s", ent.GetName(), f.Name, now.dflt)
			if now.dflt == "" {
				summary = fmt.Sprintf("%s: drop default on %s", ent.GetName(), f.Name)
			}
			alters = append(alters, SchemaChange{
				Summary: summary,
				SQL:     alterDefaultSQL(qtable, qcol, now.dflt),
				Down:    alterDefaultSQL(qtable, qcol, was.dflt),
			})
		}
	}
	return alters, changed, nil
}

// alterDefaultSQL sets a Postgres column's default to clause (" DEFAULT
// …"), or drops it for "".
func alterDefaultSQL(qtable, qcol, clause string) string {
	qtable, qcol = ddlIdent(qtable), ddlIdent(qcol)
	if clause == "" {
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", qtable, qcol)
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET%s", qtable, qcol, clause)
}
