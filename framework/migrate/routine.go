package migrate

import "github.com/DonaldMurillo/gofastr/framework/entity"

// Routine is a database function, procedure, trigger or view managed as a
// migration object. Up is idempotent SQL run verbatim at every boot; Down
// rolls it back. GenerateMigration emits a migration when Up's checksum
// changes.
type Routine struct {
	Name string // unique identifier used for change tracking
	Up   string // CREATE OR REPLACE … (run verbatim, idempotent)
	Down string // DROP … or CREATE OR REPLACE of the prior body

	// Dialect scopes the routine to one dialect; empty runs on every one, and
	// boot skips a routine whose Dialect is not the database's.
	Dialect Dialect
}

// Plan is the full migration surface AutoMigratePlanContext and GeneratePlan
// reconcile in one pass: tables, views and routines.
type Plan struct {
	Registry entity.Registry
	Views    []View
	Routines []Routine
}

// dialectString renders a Dialect for log output, "all" for the empty one.
func dialectString(d Dialect) string {
	if d == "" {
		return "all"
	}
	return string(d)
}
