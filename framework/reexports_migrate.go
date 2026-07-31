package framework

import (
	"context"
	"database/sql"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

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

// AutoMigrate wraps migrate.AutoMigrate.
func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	return migrate.AutoMigrate(db, registry)
}

// AutoMigrateContext wraps migrate.AutoMigrateContext.
func AutoMigrateContext(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	return migrate.AutoMigrateContext(ctx, db, registry)
}

// AutoMigratePlanContext wraps migrate.AutoMigratePlanContext.
func AutoMigratePlanContext(ctx context.Context, db *sql.DB, plan migrate.Plan) error {
	return migrate.AutoMigratePlanContext(ctx, db, plan)
}

// MigrateEntity wraps migrate.MigrateEntity.
func MigrateEntity(db *sql.DB, ent *entity.Entity) error { return migrate.MigrateEntity(db, ent) }

// MigrateEntityDialect wraps migrate.MigrateEntityDialect.
func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect migrate.Dialect) error {
	return migrate.MigrateEntityDialect(db, ent, dialect)
}

// DiffSchema wraps migrate.DiffSchema.
func DiffSchema(ctx context.Context, db *sql.DB, registry entity.Registry) ([]migrate.SchemaChange, error) {
	return migrate.DiffSchema(ctx, db, registry)
}

// ApplySchemaDiff wraps migrate.ApplySchemaDiff.
func ApplySchemaDiff(ctx context.Context, db *sql.DB, changes []migrate.SchemaChange) (int, error) {
	return migrate.ApplySchemaDiff(ctx, db, changes)
}

// ApplySchemaDiffWithOptions wraps migrate.ApplySchemaDiffWithOptions.
func ApplySchemaDiffWithOptions(ctx context.Context, db *sql.DB, changes []migrate.SchemaChange, opts migrate.ApplyOptions) (int, error) {
	return migrate.ApplySchemaDiffWithOptions(ctx, db, changes, opts)
}

// DetectDialect wraps migrate.DetectDialect.
func DetectDialect(db *sql.DB) migrate.Dialect { return migrate.DetectDialect(db) }

// GenerateMigration wraps migrate.GenerateMigration.
func GenerateMigration(reg entity.Registry, prev migrate.SchemaSnapshot, dialect migrate.Dialect) (up string, down string, next migrate.SchemaSnapshot, err error) {
	return migrate.GenerateMigration(reg, prev, dialect)
}

// GeneratePlan wraps migrate.GeneratePlan.
func GeneratePlan(plan migrate.Plan, prev migrate.SchemaSnapshot, dialect migrate.Dialect) (up string, down string, next migrate.SchemaSnapshot, err error) {
	return migrate.GeneratePlan(plan, prev, dialect)
}

// SnapshotFromRegistry wraps migrate.SnapshotFromRegistry.
func SnapshotFromRegistry(reg entity.Registry, dialect migrate.Dialect) migrate.SchemaSnapshot {
	return migrate.SnapshotFromRegistry(reg, dialect)
}

// SnapshotFromPlan wraps migrate.SnapshotFromPlan.
func SnapshotFromPlan(plan migrate.Plan, dialect migrate.Dialect) migrate.SchemaSnapshot {
	return migrate.SnapshotFromPlan(plan, dialect)
}

// RenderMigrationFile wraps migrate.RenderMigrationFile.
func RenderMigrationFile(version uint64, name string, up string, down string) string {
	return migrate.RenderMigrationFile(version, name, up, down)
}

// LoadSnapshot wraps migrate.LoadSnapshot.
func LoadSnapshot(path string) (migrate.SchemaSnapshot, error) { return migrate.LoadSnapshot(path) }

// SaveSnapshot wraps migrate.SaveSnapshot.
func SaveSnapshot(path string, snap migrate.SchemaSnapshot) error {
	return migrate.SaveSnapshot(path, snap)
}
