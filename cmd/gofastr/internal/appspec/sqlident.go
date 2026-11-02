package appspec

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// AutoMigrate's DDL and the CRUD layer's statements name a table or column
// bare, except a keyword (order, group, user, desc, …), which both write
// quoted and folded (query.QuoteKeyword), so a keyword is an ordinary name.
// A name bare SQL cannot carry at all is an app that generates, compiles
// and then dies at boot or on its first insert. The checks here move that
// failure to generate time, where the error can name the field.

// sqlNameProblem says why name cannot be an SQL table, column or index
// name, or returns "" when it can. Postgres truncates an identifier to
// query.MaxIdentBytes (63) bytes without a word, so two longer names that
// share their first 63 bytes would name one table or one column.
func sqlNameProblem(name string) string {
	switch {
	case name == "":
		return "is empty"
	case strings.Contains(name, "."):
		return "contains a dot"
	}
	if _, err := query.SafeIdent(name); err != nil {
		return "is not letters, digits and underscores starting with a letter or underscore"
	}
	if len(name) > query.MaxIdentBytes {
		return fmt.Sprintf("is %d bytes long: Postgres cuts a name to its first %d bytes, so two names that share those would be one", len(name), query.MaxIdentBytes)
	}
	return ""
}

// appSpecBatteryTables are the tables the batteries a generated app mounts
// keep for themselves, under the names the generator gives them
// (cmd/gofastr/internal/appgen: auth.NewEntityUserStore(db, "auth_users")
// and its _oauth_links table, auth.NewEntitySessionStore(db,
// "auth_sessions"), admin.Config's AuditTable "audit_log" and its saved
// views) plus the framework's own. An entity on one of them is migrated
// onto the battery's table: its CRUD routes then serve the battery's rows
// to every signed-in user (an entity named auth_users lists, edits and
// deletes the accounts), or the entity's columns break the battery's reads
// (an entity named auth_sessions signs every user out).
var appSpecBatteryTables = map[string]string{
	"auth_users":             "the auth battery's accounts",
	"auth_users_oauth_links": "the auth battery's OAuth links",
	"auth_sessions":          "the auth battery's sessions",
	"audit_log":              "the audit log the admin battery reads",
	"admin_saved_views":      "the admin battery's saved views",
	"gofastr_modules":        "the framework's module registry",
	"_gofastr_migrate_lock":  "the framework's migration lock",
}

// entityTable is the table App.Entity creates for decl: the declared
// Table, or the name in snake_case. Define spells the default, so the
// check reads the same name the running app will create.
func entityTable(decl framework.EntityDeclaration) string {
	return fwentity.Define(decl.Name, fwentity.EntityConfig{Table: decl.Table}).GetTable()
}

// tableKey is the name two tables are compared by: folded, since Postgres
// folds an unquoted name, and with the engine's default schema dropped,
// since public.auth_users on Postgres and main.auth_users on SQLite are the
// same table as a bare auth_users.
func tableKey(table string) string {
	folded := asciicase.Lower(table)
	for _, schema := range []string{"public.", "main."} {
		if rest, ok := strings.CutPrefix(folded, schema); ok {
			return rest
		}
	}
	return folded
}

// validateAppSpecSQLNames checks every name the entities put into SQL: each
// table (every dot-separated part of a declared one), each field's column,
// and each declared index name; and that no two entities, and no entity
// and a battery, share a table. Postgres folds an unquoted name to lower
// case, so tables are compared folded.
func validateAppSpecSQLNames(bp AppSpec) error {
	var errs schemaErrors
	tables := map[string]string{} // folded table → the entity that claimed it
	names := map[string]bool{}
	for _, decl := range bp.Entities {
		if decl.Name == "" || !project.IsGoIdentifier(ToCamelCase(decl.Name)) || names[decl.Name] {
			continue // reported by ValidateAppSpec
		}
		names[decl.Name] = true
		table := entityTable(decl)
		for part := range strings.SplitSeq(table, ".") {
			if why := sqlNameProblem(part); why != "" {
				errs.add(fmt.Errorf("app spec: entity %q table %q %s; set table: to another name", decl.Name, part, why))
			}
		}
		folded := tableKey(table)
		if owner, ok := appSpecBatteryTables[folded]; ok {
			errs.add(fmt.Errorf("app spec: entity %q uses the table %q, which is %s: the entity would be migrated onto it and serve or break its rows; rename the entity or set table: to another name", decl.Name, table, owner))
		}
		if prev, dup := tables[folded]; dup {
			errs.add(fmt.Errorf("app spec: entities %q and %q both use the table %q: one table cannot hold two entities; set table: on one of them", prev, decl.Name, table))
		} else {
			tables[folded] = decl.Name
		}
		for _, f := range decl.Fields {
			if !project.IsGoIdentifier(ToCamelCase(f.Name)) {
				continue // reported by ValidateAppSpec
			}
			if why := sqlNameProblem(f.Name); why != "" {
				errs.add(fmt.Errorf("app spec: entity %q field %q %s: the field is a column; rename the field", decl.Name, f.Name, why))
			}
		}
		for _, idx := range decl.Indices {
			if idx.Name == "" {
				continue
			}
			if why := sqlNameProblem(idx.Name); why != "" {
				errs.add(fmt.Errorf("app spec: entity %q index %q %s; rename the index", decl.Name, idx.Name, why))
			}
		}
	}
	return errs.err()
}
