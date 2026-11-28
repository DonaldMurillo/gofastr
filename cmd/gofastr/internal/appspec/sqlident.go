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

var appSpecBatteryTables = map[string]string{
	"auth_users":             "the auth battery's accounts",
	"auth_users_oauth_links": "the auth battery's OAuth links",
	"auth_sessions":          "the auth battery's sessions",
	"audit_log":              "the audit log the admin battery reads",
	"admin_saved_views":      "the admin battery's saved views",
	"gofastr_modules":        "the framework's module registry",
	"_gofastr_migrate_lock":  "the framework's migration lock",
}

func entityTable(decl framework.EntityDeclaration) string {
	return fwentity.Define(decl.Name, fwentity.EntityConfig{Table: decl.Table}).GetTable()
}

func tableKey(table string) string {
	folded := asciicase.Lower(table)
	for _, schema := range []string{"public.", "main."} {
		if rest, ok := strings.CutPrefix(folded, schema); ok {
			return rest
		}
	}
	return folded
}

func validateAppSpecSQLNames(bp AppSpec) error {
	var errs schemaErrors
	tables := map[string]string{}
	names := map[string]bool{}
	for _, decl := range bp.Entities {
		if decl.Name == "" || !project.IsGoIdentifier(ToCamelCase(decl.Name)) || names[decl.Name] {
			continue
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
				continue
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
