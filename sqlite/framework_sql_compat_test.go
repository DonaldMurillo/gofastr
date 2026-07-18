package sqlite

import (
	"database/sql"
	"testing"
)

func TestDriverCompatPostgresNumberedPlaceholders(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE records (id TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO records (id, value) VALUES ($2, $1)", "value-a", "id-a"); err != nil {
		t.Fatalf("numbered insert: %v", err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM records WHERE id=$1", "id-a").Scan(&value); err != nil {
		t.Fatalf("numbered select: %v", err)
	}
	if value != "value-a" {
		t.Fatalf("value = %q, want value-a", value)
	}
}

func TestDriverCompatFrameworkConflictForms(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE grants (
		role TEXT NOT NULL,
		permission TEXT NOT NULL,
		UNIQUE(role, permission)
	)`); err != nil {
		t.Fatalf("table unique: %v", err)
	}
	if _, err := db.Exec("INSERT INTO grants (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		"admin", "posts:read"); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if _, err := db.Exec("INSERT INTO grants (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		"admin", "posts:read"); err != nil {
		t.Fatalf("idempotent grant: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM grants").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate grant count = %d, want 1", count)
	}
}

func TestDriverCompatFrameworkUpsertReturning(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE records (id TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	var id, value string
	err := db.QueryRow(`INSERT INTO records (id, value) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET value=excluded.value
		RETURNING id, value`, "id-a", "first").Scan(&id, &value)
	if err != nil {
		t.Fatalf("insert returning: %v", err)
	}
	err = db.QueryRow(`INSERT INTO records (id, value) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET value=excluded.value
		RETURNING id, value`, "id-a", "second").Scan(&id, &value)
	if err != nil {
		t.Fatalf("update returning: %v", err)
	}
	if id != "id-a" || value != "second" {
		t.Fatalf("returning = (%q, %q), want (id-a, second)", id, value)
	}
}

func TestDriverCompatCompositePrimaryKeyAndInsertOrIgnore(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE deliveries (
		row_id TEXT NOT NULL,
		consumer TEXT NOT NULL,
		status TEXT NOT NULL,
		PRIMARY KEY (row_id, consumer)
	)`); err != nil {
		t.Fatalf("composite primary key: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(`INSERT OR IGNORE INTO deliveries
			(row_id, consumer, status) VALUES ($1, $2, $3)`,
			"row-a", "consumer-a", "pending"); err != nil {
			t.Fatalf("insert or ignore %d: %v", i, err)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM deliveries").Scan(&count); err != nil &&
		err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivery count = %d, want 1", count)
	}
}
