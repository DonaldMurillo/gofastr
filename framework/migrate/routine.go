package migrate

import "github.com/DonaldMurillo/gofastr/framework/entity"

// Routine is a database routine managed as a first-class migration object — a
// function, stored procedure, trigger, or view. Up is the idempotent
// definition (CREATE OR REPLACE …); Down rolls it back (a DROP, or a CREATE OR
// REPLACE of the previous body for true reversibility). The SQL is dialect-
// specific and run verbatim, so a `$$ … $$` body is a single statement.
//
// AutoMigrate runs every routine's Up on boot (idempotent). GenerateMigration
// tracks each routine by a checksum of its Up and emits a migration only when
// the body changes — with a Down that restores the previous definition.
type Routine struct {
	Name string // unique identifier used for change tracking
	Up   string // CREATE OR REPLACE … (run verbatim, idempotent)
	Down string // DROP … or CREATE OR REPLACE of the prior body
}

// Plan is the full migration surface: tables (entities and/or raw Tables, via a
// registry) plus stored routines. It's what AutoMigratePlanContext and
// GeneratePlan consume so non-entity tables and routines reconcile with
// entities in one pass.
type Plan struct {
	Registry entity.Registry
	Views    []View
	Routines []Routine
}
