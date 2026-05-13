package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// --- helpers ---

func newTestMigrator(t *testing.T) (*Migrator, sqlmock.Sqlmock) {
	t.Helper()
	return newTestMigratorWithDialect(t, DialectPostgres)
}

func newTestMigratorWithDialect(t *testing.T, d Dialect) (*Migrator, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	m := New(db, WithDialect(d))
	return m, mock
}

// expectCreateTable expects the CREATE TABLE IF NOT EXISTS for _migrations.
func expectCreateTable(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta(
		"CREATE TABLE IF NOT EXISTS _migrations (\n\t\tversion BIGINT NOT NULL PRIMARY KEY,\n\t\tname    TEXT    NOT NULL DEFAULT '',\n\t\tapplied_at TIMESTAMP NOT NULL DEFAULT NOW()\n\t)",
	)).WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectSelectApplied expects the query that fetches applied migrations.
// versions is the list of already-applied version numbers.
func expectSelectApplied(mock sqlmock.Sqlmock, versions []uint64) {
	rows := sqlmock.NewRows([]string{"version", "name", "applied_at"})
	for _, v := range versions {
		rows.AddRow(v, fmt.Sprintf("migration_%d", v), time.Now().UTC())
	}
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT version, name, applied_at FROM _migrations ORDER BY version",
	)).WillReturnRows(rows)
}

// expectMigrationUp expects a transaction that runs one migration up.
func expectMigrationUp(mock sqlmock.Sqlmock, version uint64, upSQL string) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(upSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(
		"INSERT INTO _migrations (version, name, applied_at) VALUES ($1, $2, $3)",
	)).WithArgs(version, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// expectMigrationDown expects a transaction that rolls back one migration.
func expectMigrationDown(mock sqlmock.Sqlmock, downSQL string, version uint64) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(downSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(
		"DELETE FROM _migrations WHERE version = $1",
	)).WithArgs(version).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// --- tests ---

func TestRegisterSortsMigrations(t *testing.T) {
	m, _ := newTestMigrator(t)

	m.Register(Migration{Version: 3, Name: "third"})
	m.Register(Migration{Version: 1, Name: "first"})
	m.Register(Migration{Version: 2, Name: "second"})

	if len(m.migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(m.migrations))
	}
	for i, want := range []uint64{1, 2, 3} {
		if m.migrations[i].Version != want {
			t.Errorf("migrations[%d].Version = %d, want %d", i, m.migrations[i].Version, want)
		}
	}
}

func TestUpRunsPendingMigrations(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	up1 := "CREATE TABLE users (id BIGSERIAL PRIMARY KEY, name TEXT)"
	up2 := "ALTER TABLE users ADD COLUMN email TEXT"

	m.Register(Migration{Version: 1, Name: "create_users", Up: up1, Down: "DROP TABLE users"})
	m.Register(Migration{Version: 2, Name: "add_email", Up: up2, Down: "ALTER TABLE users DROP COLUMN email"})

	// Expect: create table, select applied (empty), then run both migrations.
	expectCreateTable(mock)
	expectSelectApplied(mock, nil)
	expectMigrationUp(mock, 1, up1)
	expectMigrationUp(mock, 2, up2)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	up1 := "CREATE TABLE items (id BIGSERIAL PRIMARY KEY)"

	m.Register(Migration{Version: 1, Name: "create_items", Up: up1, Down: "DROP TABLE items"})

	// First Up: runs the migration.
	expectCreateTable(mock)
	expectSelectApplied(mock, nil)
	expectMigrationUp(mock, 1, up1)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}

	// Second Up: no pending migrations.
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})

	if err := m.Up(ctx); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpSkipsAlreadyApplied(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	up1 := "CREATE TABLE users (id BIGSERIAL PRIMARY KEY)"
	up2 := "ALTER TABLE users ADD COLUMN age INT"
	up3 := "ALTER TABLE users ADD COLUMN active BOOLEAN"

	m.Register(Migration{Version: 1, Name: "create_users", Up: up1, Down: "DROP TABLE users"})
	m.Register(Migration{Version: 2, Name: "add_age", Up: up2, Down: "ALTER TABLE users DROP COLUMN age"})
	m.Register(Migration{Version: 3, Name: "add_active", Up: up3, Down: "ALTER TABLE users DROP COLUMN active"})

	// Version 1 is already applied; only 2 and 3 should run.
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})
	expectMigrationUp(mock, 2, up2)
	expectMigrationUp(mock, 3, up3)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDownRollsBackLastN(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	down2 := "ALTER TABLE users DROP COLUMN email"
	down1 := "DROP TABLE users"

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users (id BIGSERIAL PRIMARY KEY)", Down: down1})
	m.Register(Migration{Version: 2, Name: "add_email", Up: "ALTER TABLE users ADD COLUMN email TEXT", Down: down2})

	// Down(2) should roll back version 2 then version 1.
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1, 2})
	expectMigrationDown(mock, down2, 2)
	expectMigrationDown(mock, down1, 1)

	if err := m.Down(ctx, 2); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDownRollsBackOne(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	down2 := "ALTER TABLE users DROP COLUMN email"

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users (id BIGSERIAL PRIMARY KEY)", Down: "DROP TABLE users"})
	m.Register(Migration{Version: 2, Name: "add_email", Up: "ALTER TABLE users ADD COLUMN email TEXT", Down: down2})

	// Down(1) should only roll back version 2 (the last applied).
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1, 2})
	expectMigrationDown(mock, down2, 2)

	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStatus(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users", Down: "DROP TABLE users"})
	m.Register(Migration{Version: 2, Name: "add_email", Up: "ALTER TABLE users ADD COLUMN email", Down: "ALTER TABLE users DROP COLUMN email"})
	m.Register(Migration{Version: 3, Name: "add_age", Up: "ALTER TABLE users ADD COLUMN age", Down: "ALTER TABLE users DROP COLUMN age"})

	// Version 1 is applied, 2 and 3 are pending.
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(status.Applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(status.Applied))
	}
	if status.Applied[0].Version != 1 {
		t.Errorf("applied[0].Version = %d, want 1", status.Applied[0].Version)
	}
	if len(status.Pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(status.Pending))
	}
	if status.Pending[0].Version != 2 {
		t.Errorf("pending[0].Version = %d, want 2", status.Pending[0].Version)
	}
	if status.Pending[1].Version != 3 {
		t.Errorf("pending[1].Version = %d, want 3", status.Pending[1].Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStatusAllApplied(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users", Down: "DROP TABLE users"})

	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(status.Applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(status.Applied))
	}
	if len(status.Pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(status.Pending))
	}
}

func TestCreateMigrationsTable(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	expectCreateTable(mock)

	if err := m.CreateMigrationsTable(ctx); err != nil {
		t.Fatalf("CreateMigrationsTable: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWithTableName(t *testing.T) {
	db, _, _ := sqlmock.New()
	m := New(db, WithTableName("custom_migrations"))
	if m.tableName != "custom_migrations" {
		t.Errorf("tableName = %q, want %q", m.tableName, "custom_migrations")
	}
}

func TestRegisterFromReader(t *testing.T) {
	m, _ := newTestMigrator(t)

	content := `-- +migrate Version 1
-- +migrate Name create_users
-- +migrate Up
CREATE TABLE users (id BIGSERIAL PRIMARY KEY, name TEXT);
-- +migrate Down
DROP TABLE users;`

	if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
		t.Fatalf("RegisterFromReader: %v", err)
	}

	if len(m.migrations) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(m.migrations))
	}
	mig := m.migrations[0]
	if mig.Version != 1 {
		t.Errorf("Version = %d, want 1", mig.Version)
	}
	if mig.Name != "create_users" {
		t.Errorf("Name = %q, want %q", mig.Name, "create_users")
	}
	if !strings.Contains(mig.Up, "CREATE TABLE users") {
		t.Errorf("Up = %q, want CREATE TABLE users", mig.Up)
	}
	if !strings.Contains(mig.Down, "DROP TABLE users") {
		t.Errorf("Down = %q, want DROP TABLE users", mig.Down)
	}
}

func TestRegisterFromReaderMissingVersion(t *testing.T) {
	m, _ := newTestMigrator(t)

	content := `-- +migrate Up
CREATE TABLE foo (id INT);
-- +migrate Down
DROP TABLE foo;`

	err := m.RegisterFromReader(strings.NewReader(content))
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestFieldTypeToSQL(t *testing.T) {
	tests := []struct {
		ft   schema.FieldType
		want string
	}{
		{schema.String, "VARCHAR(255)"},
		{schema.Text, "TEXT"},
		{schema.Int, "BIGINT"},
		{schema.Float, "DOUBLE PRECISION"},
		{schema.Bool, "BOOLEAN"},
		{schema.UUID, "UUID"},
		{schema.Timestamp, "TIMESTAMP"},
		{schema.Date, "DATE"},
		{schema.JSON, "JSONB"},
		{schema.Relation, "UUID"},
		{schema.Image, "TEXT"},
		{schema.File, "TEXT"},
	}
	for _, tt := range tests {
		got := fieldTypeToSQL(tt.ft)
		if got != tt.want {
			t.Errorf("fieldTypeToSQL(%v) = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

func TestDownWithNoApplied(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users", Down: "DROP TABLE users"})

	expectCreateTable(mock)
	expectSelectApplied(mock, nil)

	// Down(1) with nothing applied should be a no-op.
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDownClampsToAppliedCount(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()

	down1 := "DROP TABLE users"

	m.Register(Migration{Version: 1, Name: "create_users", Up: "CREATE TABLE users", Down: down1})

	// Request Down(5) but only 1 is applied; should roll back 1.
	expectCreateTable(mock)
	expectSelectApplied(mock, []uint64{1})
	expectMigrationDown(mock, down1, 1)

	if err := m.Down(ctx, 5); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// --- Dialect tests ---

func TestDefaultDialectIsPostgres(t *testing.T) {
	db, _, _ := sqlmock.New()
	m := New(db)
	if m.dialect != DialectPostgres {
		t.Errorf("default dialect = %q, want %q", m.dialect, DialectPostgres)
	}
}

func TestWithDialect(t *testing.T) {
	db, _, _ := sqlmock.New()
	m := New(db, WithDialect(DialectSQLite))
	if m.dialect != DialectSQLite {
		t.Errorf("dialect = %q, want %q", m.dialect, DialectSQLite)
	}
}

func TestPlaceholder(t *testing.T) {
	db, _, _ := sqlmock.New()

	pg := New(db, WithDialect(DialectPostgres))
	if got := pg.placeholder(1); got != "$1" {
		t.Errorf("postgres placeholder(1) = %q, want $1", got)
	}
	if got := pg.placeholder(3); got != "$3" {
		t.Errorf("postgres placeholder(3) = %q, want $3", got)
	}

	sqlite := New(db, WithDialect(DialectSQLite))
	if got := sqlite.placeholder(1); got != "?" {
		t.Errorf("sqlite placeholder(1) = %q, want ?", got)
	}
	if got := sqlite.placeholder(3); got != "?" {
		t.Errorf("sqlite placeholder(3) = %q, want ?", got)
	}
}

func TestNowFunc(t *testing.T) {
	db, _, _ := sqlmock.New()

	pg := New(db, WithDialect(DialectPostgres))
	if got := pg.nowFunc(); got != "NOW()" {
		t.Errorf("postgres nowFunc() = %q, want NOW()", got)
	}

	sqlite := New(db, WithDialect(DialectSQLite))
	if got := sqlite.nowFunc(); got != "CURRENT_TIMESTAMP" {
		t.Errorf("sqlite nowFunc() = %q, want CURRENT_TIMESTAMP", got)
	}
}

func TestCreateMigrationsTableDDL(t *testing.T) {
	pg := New(nil, WithDialect(DialectPostgres))
	if got := pg.nowFunc(); got != "NOW()" {
		t.Errorf("pg nowFunc = %q, want NOW()", got)
	}

	sqlite := New(nil, WithDialect(DialectSQLite))
	if got := sqlite.nowFunc(); got != "CURRENT_TIMESTAMP" {
		t.Errorf("sqlite nowFunc = %q, want CURRENT_TIMESTAMP", got)
	}
}

func TestSQLiteIntegration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := New(db, WithDialect(DialectSQLite))

	m.Register(Migration{
		Version: 1,
		Name:    "create_users",
		Up:      "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		Down:    "DROP TABLE IF EXISTS users",
	})
	m.Register(Migration{
		Version: 2,
		Name:    "add_email",
		Up:      "ALTER TABLE users ADD COLUMN email TEXT",
		Down:    "ALTER TABLE users DROP COLUMN email",
	})

	ctx := context.Background()

	// Run Up.
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Verify migrations table was created and both migrations applied.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", count)
	}

	// Verify the users table has the email column.
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info('users') ORDER BY cid")
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns = append(columns, colName)
	}

	expectedCols := map[string]bool{"id": true, "name": true, "email": true}
	for _, col := range columns {
		if !expectedCols[col] {
			t.Errorf("unexpected column %q", col)
		}
		delete(expectedCols, col)
	}
	if len(expectedCols) > 0 {
		t.Errorf("missing columns: %v", expectedCols)
	}

	// Verify status.
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Applied) != 2 {
		t.Fatalf("expected 2 applied, got %d", len(status.Applied))
	}
	if len(status.Pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(status.Pending))
	}

	// Down(1) should roll back version 2 only.
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down(1): %v", err)
	}

	status, err = m.Status(ctx)
	if err != nil {
		t.Fatalf("Status after down: %v", err)
	}
	if len(status.Applied) != 1 {
		t.Fatalf("expected 1 applied after down, got %d", len(status.Applied))
	}
	if status.Applied[0].Version != 1 {
		t.Errorf("remaining version = %d, want 1", status.Applied[0].Version)
	}

	// Up again should re-apply version 2.
	if err := m.Up(ctx); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	status, err = m.Status(ctx)
	if err != nil {
		t.Fatalf("Status after second up: %v", err)
	}
	if len(status.Applied) != 2 {
		t.Fatalf("expected 2 applied after re-up, got %d", len(status.Applied))
	}
}

func TestSQLiteCreateMigrationsTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := New(db, WithDialect(DialectSQLite))
	ctx := context.Background()

	if err := m.CreateMigrationsTable(ctx); err != nil {
		t.Fatalf("CreateMigrationsTable: %v", err)
	}

	// Verify the table exists and has the right columns.
	rows, err := db.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='_migrations'")
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("_migrations table not found")
	}
	var ddl string
	if err := rows.Scan(&ddl); err != nil {
		t.Fatalf("scan ddl: %v", err)
	}
	if !strings.Contains(ddl, "CURRENT_TIMESTAMP") {
		t.Errorf("DDL missing CURRENT_TIMESTAMP: %s", ddl)
	}
	if strings.Contains(ddl, "NOW()") {
		t.Errorf("DDL should not contain NOW() for sqlite: %s", ddl)
	}
}

func TestGenerateCreateTable(t *testing.T) {
	m, _ := newTestMigrator(t)

	s := schema.Schema{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "age", Type: schema.Int},
		},
	}

	ddl := m.generateCreateTable("users", s)

	if !strings.Contains(ddl, "CREATE TABLE users") {
		t.Errorf("missing CREATE TABLE users in: %s", ddl)
	}
	if !strings.Contains(ddl, "id BIGSERIAL PRIMARY KEY") {
		t.Errorf("missing auto id column in: %s", ddl)
	}
	if !strings.Contains(ddl, "name VARCHAR(255) NOT NULL") {
		t.Errorf("missing name column in: %s", ddl)
	}
	if !strings.Contains(ddl, "age BIGINT") {
		t.Errorf("missing age column in: %s", ddl)
	}
	if !strings.Contains(ddl, "created_at TIMESTAMP") {
		t.Errorf("missing created_at in: %s", ddl)
	}
	if !strings.Contains(ddl, "updated_at TIMESTAMP") {
		t.Errorf("missing updated_at in: %s", ddl)
	}
}
