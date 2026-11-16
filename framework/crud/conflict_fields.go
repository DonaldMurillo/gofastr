package crud

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

const (
	sqliteUniqueLead  = "UNIQUE constraint failed: "
	sqliteNotNullLead = "NOT NULL constraint failed: "
	pgNotNullLead     = `null value in column "`
	mysqlNotNullLead  = "Column '"
	pgForeignKeyLead  = "insert or update on table "
	mysqlForeignChild = "Cannot add or update a child row"
)

type constraintFieldsError struct {
	err    error
	fields map[string][]schema.Violation
}

func (e *constraintFieldsError) Error() string { return e.err.Error() }
func (e *constraintFieldsError) Unwrap() error { return e.err }

// withConstraintFields wraps a constraint refusal with the declared
// fields it names; sent holds the body's keys from before the write.
func (ch *CrudHandler) withConstraintFields(err error, sent map[string]bool) error {
	msg := schema.ReasonUniqueTaken
	switch {
	case err == nil:
		return nil
	case isNotNullViolation(err):
		c := notNullColumn(tableBase(ch.Entity.Config.Table), err.Error())
		f, declared := fieldNamed(ch.Entity, c)
		if !declared || f.Hidden {
			return err
		}
		return &constraintFieldsError{err: err, fields: map[string][]schema.Violation{ch.convertKey(f.Name): {{Reason: schema.ReasonRequired}}}}
	case isUniqueViolation(err):
	case isForeignKeyViolation(err):
		msg = reasonMissingTarget
	default:
		return err
	}
	cols := conflictColumns(ch.Entity, err.Error())
	fields := map[string][]schema.Violation{}
	for _, c := range cols {
		f, declared := fieldNamed(ch.Entity, c)
		key := ch.convertKey(c)
		if !declared || !(sent[c] || sent[key]) {
			continue
		}
		if f.Hidden || f.NoQuery {
			return err
		}
		fields[key] = []schema.Violation{{Reason: msg}}
	}
	if len(fields) == 0 {
		return err
	}
	return &constraintFieldsError{err: err, fields: fields}
}

func conflictRefusal(ctx context.Context, err error, reason i18n.Phrase) refusal {
	cf, ok := errors.AsType[*constraintFieldsError](err)
	if !ok {
		return phraseRefusal(ctx, http.StatusConflict, reason)
	}
	return fieldsRefusal(ctx, http.StatusConflict, schema.Violation{Reason: reason}, cf.fields, nil)
}

func sentKeys(body map[string]any) map[string]bool {
	out := make(map[string]bool, len(body))
	for k := range body {
		out[k] = true
	}
	return out
}

// fieldNamed finds the field a driver named, folded or not.
func fieldNamed(e *entity.Entity, col string) (schema.Field, bool) {
	for _, f := range e.Config.Fields {
		if f.Name == col || f.Name == foldIdent(col) || foldIdent(f.Name) == col {
			return f, true
		}
	}
	return schema.Field{}, false
}

// constraintGroup is one declared constraint; keys are names only MySQL
// reports it by.
type constraintGroup struct {
	cols  []string
	names []string
	keys  []string
}

// uniqueGroups lists the entity's Unique fields and unique column
// indices under every name a driver may report them by.
func uniqueGroups(e *entity.Entity) []constraintGroup {
	table := tableBase(e.Config.Table)
	legacy := map[string]int{}
	for _, f := range e.Config.Fields {
		if f.Unique {
			legacy[foldIdent(entity.LegacyUniqueKeyName(table, f.Name))]++
		}
	}
	var gs []constraintGroup
	for _, f := range e.Config.Fields {
		if !f.Unique {
			continue
		}
		names := []string{entity.UniqueKeyName(table, f.Name)}
		if old := entity.LegacyUniqueKeyName(table, f.Name); old != names[0] && legacy[foldIdent(old)] == 1 {
			names = append(names, old)
		}
		gs = append(gs, constraintGroup{cols: []string{f.Name}, names: withFolded(names...), keys: withFolded(f.Name)})
	}
	for _, ix := range e.Config.Indices {
		if !ix.Unique || ix.Expression != "" || len(ix.Columns) == 0 {
			continue
		}
		name := ix.Name
		if name == "" {
			name = entity.DefaultIndexName(table, ix.Columns)
		}
		gs = append(gs, constraintGroup{cols: ix.Columns, names: withFolded(name)})
	}
	return gs
}

func mysqlKeyIs(msg, table, n string) bool {
	return strings.HasSuffix(msg, "for key '"+n+"'") || strings.HasSuffix(msg, "for key '"+table+"."+n+"'")
}

// conflictColumns returns the columns of the declared constraint a
// driver's refusal names, or nil.
func conflictColumns(e *entity.Entity, msg string) []string {
	table := tableBase(e.Config.Table)
	if i := strings.Index(msg, sqliteUniqueLead); i >= 0 {
		rest := msg[i+len(sqliteUniqueLead):]
		if j := strings.Index(rest, " ("); j >= 0 {
			rest = rest[:j]
		}
		var cols []string
		for c := range strings.SplitSeq(rest, ", ") {
			cols = append(cols, strings.TrimPrefix(strings.TrimSpace(c), table+"."))
		}
		for _, g := range uniqueGroups(e) {
			if sameSet(g.cols, cols) {
				return g.cols
			}
		}
		return nil
	}
	if isUniqueViolation(errString(msg)) {
		for _, g := range uniqueGroups(e) {
			for _, n := range g.names {
				if strings.Contains(msg, `constraint "`+n+`"`) || mysqlKeyIs(msg, table, n) {
					return g.cols
				}
			}
			for _, n := range g.keys {
				if mysqlKeyIs(msg, table, n) {
					return g.cols
				}
			}
		}
		return nil
	}
	pg := strings.Contains(msg, pgForeignKeyLead+`"`+table+`"`) || strings.Contains(msg, pgForeignKeyLead+`"`+foldIdent(table)+`"`)
	my := strings.Contains(msg, mysqlForeignChild) && strings.Contains(msg, "`"+table+"`")
	if !pg && !my {
		return nil
	}
	fkeys := map[string]int{}
	for _, f := range e.Config.Fields {
		if f.Type == schema.Relation && !f.Many {
			fkeys[foldIdent(query.ObjectName(table, f.Name, "fkey"))]++
		}
	}
	for _, f := range e.Config.Fields {
		if f.Type != schema.Relation || f.Many {
			continue
		}
		fkey := query.ObjectName(table, f.Name, "fkey")
		if pg && fkeys[foldIdent(fkey)] > 1 {
			continue
		}
		if (pg && (strings.Contains(msg, `"`+fkey+`"`) || strings.Contains(msg, `"`+foldIdent(fkey)+`"`))) ||
			(my && strings.Contains(msg, "FOREIGN KEY (`"+f.Name+"`)")) {
			return []string{f.Name}
		}
	}
	return nil
}

// tableBase drops the schema: app.inv is inv.
func tableBase(table string) string {
	return table[strings.LastIndex(table, ".")+1:]
}

func isNotNullViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, sqliteNotNullLead) || strings.Contains(msg, "violates not-null constraint") ||
		strings.Contains(msg, "Error 1048")
}

// notNullColumn returns the column a NOT NULL refusal on table names, or
// "" when it names another table's or none.
func notNullColumn(table, msg string) string {
	if i := strings.Index(msg, sqliteNotNullLead); i >= 0 {
		rest := msg[i+len(sqliteNotNullLead):]
		if j := strings.Index(rest, " ("); j >= 0 {
			rest = rest[:j]
		}
		col, ok := strings.CutPrefix(strings.TrimSpace(rest), table+".")
		if !ok {
			return ""
		}
		return col
	}
	if i := strings.Index(msg, pgNotNullLead); i >= 0 {
		col, rest, ok := strings.Cut(msg[i+len(pgNotNullLead):], `"`)
		if !ok || (strings.Contains(rest, ` of relation "`) && !strings.Contains(rest, ` of relation "`+table+`"`) &&
			!strings.Contains(rest, ` of relation "`+foldIdent(table)+`"`)) {
			return ""
		}
		return col
	}
	if i := strings.Index(msg, mysqlNotNullLead); i >= 0 && strings.Contains(msg, "cannot be null") {
		col, _, _ := strings.Cut(msg[i+len(mysqlNotNullLead):], "'")
		return col
	}
	return ""
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

type errString string

func (e errString) Error() string { return string(e) }

func withFolded(names ...string) []string {
	out := names
	for _, n := range names {
		if f := foldIdent(n); f != n {
			out = append(out, f)
		}
	}
	return out
}
