package framework

import "github.com/DonaldMurillo/gofastr/framework/migrate"

// Re-exports of framework/migrate so tests, generated code, and external
// callers using framework.X keep compiling after the migrate package
// extraction.

type (
	Dialect      = migrate.Dialect
	SchemaChange = migrate.SchemaChange
)

const (
	DialectPostgres = migrate.DialectPostgres
	DialectSQLite   = migrate.DialectSQLite
)

var (
	AutoMigrate          = migrate.AutoMigrate
	MigrateEntity        = migrate.MigrateEntity
	MigrateEntityDialect = migrate.MigrateEntityDialect
	DiffSchema           = migrate.DiffSchema
	ApplySchemaDiff      = migrate.ApplySchemaDiff
	DetectDialect        = migrate.DetectDialect
)
