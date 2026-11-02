package appspec

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// AutoMigrate writes its DDL with bare identifiers (quoting would make
// Postgres keep a declared "Widgets" while the rest of the stack folds
// it), and the CRUD layer's statements name columns the same way. So
// every table, column and index name an app spec declares reaches SQL
// unquoted, and a name that bare SQL cannot carry is an app that
// generates, compiles and then dies at boot ("auto-migrate: near
// "order": syntax error"). The checks here move that failure to
// generate time, where the error can name the field.

// sqlReservedWords are the words SQLite or Postgres refuse as a bare
// table, column or index name. The list was taken by running CREATE TABLE
// and SELECT … WHERE … ORDER BY with each SQLite keyword and each
// Postgres reserved, type/function and column-name keyword
// (pg_get_keywords catcodes R, T and C) as a table name and as a column
// name, on both engines, and keeping every word either engine refused.
// TestSQLReservedWordsBreakAutoMigrate proves each entry against
// AutoMigrate itself, on SQLite and (with TEST_POSTGRES_DSN) Postgres.
var sqlReservedWords = map[string]bool{
	"add": true, "all": true, "alter": true, "analyse": true, "analyze": true,
	"and": true, "any": true, "array": true, "as": true, "asc": true,
	"asymmetric": true, "authorization": true, "autoincrement": true,
	"between": true, "binary": true, "both": true, "case": true, "cast": true,
	"check": true, "collate": true, "collation": true, "column": true,
	"commit": true, "concurrently": true, "constraint": true, "create": true,
	"cross": true, "current_catalog": true, "current_date": true,
	"current_role": true, "current_schema": true, "current_time": true,
	"current_timestamp": true, "current_user": true, "default": true,
	"deferrable": true, "delete": true, "desc": true, "distinct": true,
	"do": true, "drop": true, "else": true, "end": true, "escape": true,
	"except": true, "exists": true, "false": true, "fetch": true, "for": true,
	"foreign": true, "freeze": true, "from": true, "full": true, "grant": true,
	"group": true, "having": true, "if": true, "ilike": true, "in": true,
	"index": true, "initially": true, "inner": true, "insert": true,
	"intersect": true, "into": true, "is": true, "isnull": true, "join": true,
	"lateral": true, "leading": true, "left": true, "like": true, "limit": true,
	"localtime": true, "localtimestamp": true, "natural": true, "not": true,
	"nothing": true, "notnull": true, "null": true, "offset": true, "on": true,
	"only": true, "or": true, "order": true, "outer": true, "overlaps": true,
	"placing": true, "primary": true, "raise": true, "references": true,
	"returning": true, "right": true, "select": true, "session_user": true,
	"set": true, "similar": true, "some": true, "symmetric": true,
	"system_user": true, "table": true, "tablesample": true, "then": true,
	"to": true, "trailing": true, "transaction": true, "true": true,
	"union": true, "unique": true, "update": true, "user": true, "using": true,
	"values": true, "variadic": true, "verbose": true, "when": true,
	"where": true, "window": true, "with": true,
}

// sqlNameProblem says why name cannot be a bare SQL table, column or index
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
	if sqlReservedWords[asciicase.Lower(name)] {
		return "is an SQL reserved word that SQLite or Postgres refuses as a bare name"
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
		folded := asciicase.Lower(table)
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
				errs.add(fmt.Errorf("app spec: entity %q field %q %s: the field is a column, and AutoMigrate writes column names bare; rename the field", decl.Name, f.Name, why))
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

// sqlReservedWordList is sqlReservedWords sorted, for tests and docs.
func sqlReservedWordList() []string {
	return slices.Sorted(maps.Keys(sqlReservedWords))
}
