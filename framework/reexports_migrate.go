package framework

import (
	"context"
	"database/sql"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Root spellings of framework/migrate.

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
	MigrationFileOptions   = migrate.MigrationFileOptions
)

const (
	DialectPostgres = migrate.DialectPostgres
	DialectSQLite   = migrate.DialectSQLite
)

func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	return migrate.AutoMigrate(db, registry)
}

func AutoMigrateContext(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	return migrate.AutoMigrateContext(ctx, db, registry)
}

func AutoMigratePlanContext(ctx context.Context, db *sql.DB, plan migrate.Plan) error {
	return migrate.AutoMigratePlanContext(ctx, db, plan)
}

func MigrateEntity(db *sql.DB, ent *entity.Entity) error { return migrate.MigrateEntity(db, ent) }

func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect migrate.Dialect) error {
	return migrate.MigrateEntityDialect(db, ent, dialect)
}

func DiffSchema(ctx context.Context, db *sql.DB, registry entity.Registry) ([]migrate.SchemaChange, error) {
	return migrate.DiffSchema(ctx, db, registry)
}

func ApplySchemaDiff(ctx context.Context, db *sql.DB, changes []migrate.SchemaChange) (int, error) {
	return migrate.ApplySchemaDiff(ctx, db, changes)
}

func ApplySchemaDiffWithOptions(ctx context.Context, db *sql.DB, changes []migrate.SchemaChange, opts migrate.ApplyOptions) (int, error) {
	return migrate.ApplySchemaDiffWithOptions(ctx, db, changes, opts)
}

func DetectDialect(db *sql.DB) migrate.Dialect { return migrate.DetectDialect(db) }

func GenerateMigration(reg entity.Registry, prev migrate.SchemaSnapshot, dialect migrate.Dialect) (up string, down string, next migrate.SchemaSnapshot, err error) {
	return migrate.GenerateMigration(reg, prev, dialect)
}

func GeneratePlan(plan migrate.Plan, prev migrate.SchemaSnapshot, dialect migrate.Dialect) (up string, down string, next migrate.SchemaSnapshot, err error) {
	return migrate.GeneratePlan(plan, prev, dialect)
}

func SnapshotFromRegistry(reg entity.Registry, dialect migrate.Dialect) migrate.SchemaSnapshot {
	return migrate.SnapshotFromRegistry(reg, dialect)
}

func SnapshotFromPlan(plan migrate.Plan, dialect migrate.Dialect) migrate.SchemaSnapshot {
	return migrate.SnapshotFromPlan(plan, dialect)
}

func RenderMigrationFile(version uint64, name string, up string, down string) string {
	return migrate.RenderMigrationFile(version, name, up, down)
}

func LoadSnapshot(path string) (migrate.SchemaSnapshot, error) { return migrate.LoadSnapshot(path) }

func SaveSnapshot(path string, snap migrate.SchemaSnapshot) error {
	return migrate.SaveSnapshot(path, snap)
}

func GenerateMigrationFile(plan migrate.Plan, name string, opts migrate.MigrationFileOptions) (string, error) {
	return migrate.GenerateMigrationFile(plan, name, opts)
}
