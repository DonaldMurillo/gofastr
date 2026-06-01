package framework

import "github.com/DonaldMurillo/gofastr/framework/migrate"

// Re-exports of framework/migrate so tests, generated code, and external
// callers using framework.X keep compiling after the migrate package
// extraction.

type (
	Dialect                = migrate.Dialect
	SchemaChange           = migrate.SchemaChange
	ApplyOptions           = migrate.ApplyOptions
	DestructiveChangeError = migrate.DestructiveChangeError
	SchemaSnapshot         = migrate.SchemaSnapshot
	Table                  = migrate.Table
	Column                 = migrate.Column
	ForeignKey             = migrate.ForeignKey
	Routine                = migrate.Routine
	View                   = migrate.View
	MigrationPlan          = migrate.Plan
)

const (
	DialectPostgres = migrate.DialectPostgres
	DialectSQLite   = migrate.DialectSQLite
)

var (
	AutoMigrate                = migrate.AutoMigrate
	AutoMigrateContext         = migrate.AutoMigrateContext
	AutoMigratePlanContext     = migrate.AutoMigratePlanContext
	MigrateEntity              = migrate.MigrateEntity
	MigrateEntityDialect       = migrate.MigrateEntityDialect
	DiffSchema                 = migrate.DiffSchema
	ApplySchemaDiff            = migrate.ApplySchemaDiff
	ApplySchemaDiffWithOptions = migrate.ApplySchemaDiffWithOptions
	DetectDialect              = migrate.DetectDialect
	GenerateMigration          = migrate.GenerateMigration
	GeneratePlan               = migrate.GeneratePlan
	SnapshotFromRegistry       = migrate.SnapshotFromRegistry
	SnapshotFromPlan           = migrate.SnapshotFromPlan
	RenderMigrationFile        = migrate.RenderMigrationFile
	LoadSnapshot               = migrate.LoadSnapshot
	SaveSnapshot               = migrate.SaveSnapshot
)
