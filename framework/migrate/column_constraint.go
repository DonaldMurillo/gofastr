package migrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// colConstraint is a column's NOT NULL and " DEFAULT …" clause ("" for none).
type colConstraint struct {
	notNull bool
	dflt    string
}

// declaredConstraint is f's NOT NULL and DEFAULT as columnDefs writes them.
func declaredConstraint(f schema.Field, keys []string, dialect Dialect, colType string) colConstraint {
	inKey := slices.Contains(keys, f.Name)
	rowid := len(keys) == 1 && strings.EqualFold(strings.TrimSpace(colType), "INTEGER")
	return colConstraint{
		notNull: (f.Required && f.AutoGenerate == schema.AutoNone) || (inKey && len(keys) > 1) ||
			(inKey && dialect == DialectSQLite && !rowid),
		dflt: ColumnDefaultClause(f, dialect),
	}
}

// recordedConstraints reads the NOT NULL and DEFAULT of each column a
// buildCreateTableSQL statement defines, keyed by lower-cased name.
func recordedConstraints(ddl string) (map[string]colConstraint, bool) {
	defs, ok := recordedColumnDefs(ddl)
	if !ok {
		return nil, false
	}
	out := map[string]colConstraint{}
	for _, def := range defs {
		c := colConstraint{notNull: strings.Contains(" "+def.head+" ", " NOT NULL ")}
		if def.dflt != "" {
			c.dflt = " DEFAULT " + def.dflt
		}
		out[strings.ToLower(def.name)] = c
	}
	return out, true
}

type recordedColumnDef struct {
	name, head, dflt string
}

// recordedColumnDefs reads the column definitions of a buildCreateTableSQL
// statement, leaving out its PRIMARY KEY and FOREIGN KEY table constraints.
func recordedColumnDefs(ddl string) ([]recordedColumnDef, bool) {
	_, body, ok := strings.Cut(ddl, " (\n\t")
	if !ok {
		return nil, false
	}
	var out []recordedColumnDef
	for _, def := range splitColumnDefs(strings.TrimSuffix(body, "\n)")) {
		name, rest, _ := strings.Cut(def, " ")
		if (strings.EqualFold(name, "PRIMARY") || strings.EqualFold(name, "FOREIGN")) && strings.HasPrefix(strings.ToUpper(rest), "KEY") {
			continue
		}
		head, dflt, _ := strings.Cut(rest, " DEFAULT ")
		out = append(out, recordedColumnDef{name: unquoteDDLIdent(name), head: head, dflt: dflt})
	}
	return out, true
}

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
// DEFAULT with prevDDL's: one ALTER per change on Postgres, the changed
// columns on SQLite.
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
		now := declaredConstraint(f, keys, dialect, "")
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

func alterDefaultSQL(qtable, qcol, clause string) string {
	qtable, qcol = ddlIdent(qtable), ddlIdent(qcol)
	if clause == "" {
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", qtable, qcol)
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET%s", qtable, qcol, clause)
}
