package crud

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A write a UNIQUE or FOREIGN KEY constraint refuses answers 409 with
// the refusal on the fields the caller sent, in the validation shape
// ({error, fields}), so a form shows it beside the control that caused
// it instead of a bare "conflict". The constraint the driver reports is
// matched against the entity's own declaration (a Unique field, a
// unique column index, a relation field), and only declared field names
// reach the response: the driver's constraint, table and value text
// never do. Columns the caller did not send (the owner or tenant column
// of a per-account index, hidden or not) are not theirs to fix and are
// not named. A Hidden or NoQuery field the caller sent is sensitive: a
// conflict on it stays bare, so a probe cannot learn which value exists.
//
// A NOT NULL refusal on a declared field (a column the database holds
// NOT NULL that the entity does not mark Required) answers 400 with
// "is required" on it, sent or not: it says nothing about stored rows.
// On a Hidden column the server fills, it is the server's own bug and
// stays a 500.

const (
	msgUniqueTaken    = "is already in use"
	msgMissingTarget  = "refers to a record that does not exist"
	msgRequired       = "is required"
	sqliteUniqueLead  = "UNIQUE constraint failed: "
	sqliteNotNullLead = "NOT NULL constraint failed: "
	pgNotNullLead     = `null value in column "`
	mysqlNotNullLead  = "Column '"
	pgForeignKeyLead  = "insert or update on table "
	mysqlForeignChild = "Cannot add or update a child row"
)

// constraintFieldsError carries the fields a constraint refusal is about
// to writeCRUDError, which keeps its order of arms.
type constraintFieldsError struct {
	err    error
	fields map[string][]string
}

func (e *constraintFieldsError) Error() string { return e.err.Error() }
func (e *constraintFieldsError) Unwrap() error { return e.err }

// withConstraintFields wraps a constraint refusal with the sent fields it
// names; any other error, or one it can name no field for, passes
// through unchanged. sent holds the request body's keys (columns, or
// wire keys), taken before the write ran.
func (ch *CrudHandler) withConstraintFields(err error, sent map[string]bool) error {
	msg := msgUniqueTaken
	switch {
	case err == nil:
		return nil
	case isNotNullViolation(err):
		c := notNullColumn(ch.Entity.Config.Table, err.Error())
		if f, declared := fieldNamed(ch.Entity, c); !declared || f.Hidden {
			return err
		}
		return &constraintFieldsError{err: err, fields: map[string][]string{ch.convertKey(c): {msgRequired}}}
	case isUniqueViolation(err):
	case isForeignKeyViolation(err):
		msg = msgMissingTarget
	default:
		return err
	}
	cols := conflictColumns(ch.Entity, err.Error())
	fields := map[string][]string{}
	for _, c := range cols {
		f, declared := fieldNamed(ch.Entity, c)
		key := ch.convertKey(c)
		if !declared || !(sent[c] || sent[key]) {
			continue
		}
		if f.Hidden || f.NoQuery {
			return err
		}
		fields[key] = []string{msg}
	}
	if len(fields) == 0 {
		return err
	}
	return &constraintFieldsError{err: err, fields: fields}
}

// writeConflict answers a constraint refusal: 409 with the generic
// message, plus the named fields when the error carries them.
func writeConflict(w http.ResponseWriter, err error, message string) {
	cf, ok := errors.AsType[*constraintFieldsError](err)
	if !ok {
		writeJSONError(w, http.StatusConflict, message)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   message,
		"success": false,
		"code":    http.StatusConflict,
		"fields":  cf.fields,
	})
}

// sentKeys snapshots a request body's keys before the write runs.
func sentKeys(body map[string]any) map[string]bool {
	out := make(map[string]bool, len(body))
	for k := range body {
		out[k] = true
	}
	return out
}

func fieldNamed(e *entity.Entity, col string) (schema.Field, bool) {
	for _, f := range e.Config.Fields {
		if f.Name == col {
			return f, true
		}
	}
	return schema.Field{}, false
}

// constraintGroup is one declared constraint: the columns it covers and
// the names a driver may report it by.
type constraintGroup struct {
	cols  []string
	names []string
}

// uniqueGroups lists the entity's unique constraints: each Unique field
// alone (Postgres names it <table>_<col>_key, MySQL by the column), then
// each unique column index under its name or the idx_<table>_<cols>
// name AutoMigrate gives it. An expression index names no columns and
// is skipped.
func uniqueGroups(e *entity.Entity) []constraintGroup {
	table := e.Config.Table
	var gs []constraintGroup
	for _, f := range e.Config.Fields {
		if f.Unique {
			gs = append(gs, constraintGroup{cols: []string{f.Name}, names: []string{table + "_" + f.Name + "_key", f.Name}})
		}
	}
	for _, ix := range e.Config.Indices {
		if !ix.Unique || ix.Expression != "" || len(ix.Columns) == 0 {
			continue
		}
		name := ix.Name
		if name == "" {
			name = "idx_" + table + "_" + strings.Join(ix.Columns, "_")
		}
		gs = append(gs, constraintGroup{cols: ix.Columns, names: []string{name}})
	}
	return gs
}

// conflictColumns returns the columns of the declared constraint a
// driver's refusal names, or nil when it names none the entity declares.
// SQLite lists a unique constraint's columns; Postgres and MySQL name
// the constraint or key, and name a foreign key by its column. SQLite's
// foreign-key refusal names nothing, so it stays bare.
func conflictColumns(e *entity.Entity, msg string) []string {
	table := e.Config.Table
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
				if strings.Contains(msg, `constraint "`+n+`"`) || strings.HasSuffix(msg, "for key '"+n+"'") || strings.HasSuffix(msg, "for key '"+table+"."+n+"'") {
					return g.cols
				}
			}
		}
		return nil
	}
	pg := strings.Contains(msg, pgForeignKeyLead+`"`+table+`"`)
	my := strings.Contains(msg, mysqlForeignChild) && strings.Contains(msg, "`"+table+"`")
	if !pg && !my {
		return nil
	}
	for _, f := range e.Config.Fields {
		if f.Type != schema.Relation || f.Many {
			continue
		}
		if (pg && strings.Contains(msg, `"`+table+"_"+f.Name+`_fkey"`)) ||
			(my && strings.Contains(msg, "FOREIGN KEY (`"+f.Name+"`)")) {
			return []string{f.Name}
		}
	}
	return nil
}

// isNotNullViolation reports whether err is a NOT NULL refusal: SQLite
// (extended code 1299), Postgres SQLSTATE 23502, MySQL error 1048.
func isNotNullViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, sqliteNotNullLead) || strings.Contains(msg, "violates not-null constraint") ||
		strings.Contains(msg, "Error 1048")
}

// notNullColumn returns the column a NOT NULL refusal on table names, or
// "" when it names another table's or none. SQLite writes table.col,
// Postgres the column and (since 12) its relation, MySQL the column.
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
		if !ok || (strings.Contains(rest, ` of relation "`) && !strings.Contains(rest, ` of relation "`+table+`"`)) {
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
