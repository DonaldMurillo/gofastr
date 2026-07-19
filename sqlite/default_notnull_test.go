package sqlite

import (
	"database/sql"
	"strings"
	"testing"
)

// ============================================================================
// Regression tests for issue #115: the bundled SQLite adapter must apply a
// column's declared DEFAULT to *omitted* columns before enforcing NOT NULL,
// must recognize TRUE/FALSE boolean literals as defaults, and must still
// reject an *explicitly supplied* NULL on a NOT NULL column that has a
// default.
//
// These tests drive the bundled pure-Go adapter end-to-end through
// database/sql + Open(), mirroring driver_compat_test.go.
// ============================================================================

// TestDefaultBooleanLiteralsBeforeNotNull reproduces the exact failure from
// the issue: an omitted NOT NULL column with DEFAULT false used to fire
// "NOT NULL constraint failed" because true/false were parsed as column
// references (and then failed to evaluate), leaving the column's Default
// pointer nil.
func TestDefaultBooleanLiteralsBeforeNotNull(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE comments (
		id TEXT PRIMARY KEY,
		subject_deleted BOOLEAN NOT NULL DEFAULT false,
		is_published BOOLEAN NOT NULL DEFAULT true
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Omit both defaulted NOT NULL columns: the defaults must apply before
	// the NOT NULL check fires.
	res, err := db.Exec(`INSERT INTO comments (id) VALUES (?)`, "abc")
	if err != nil {
		t.Fatalf("insert with omitted boolean defaults failed: %v", err)
	}
	// The row was materialized: LastInsertId must succeed and point at it.
	// (For a TEXT PK the engine reports the synthetic rowid; the value
	// itself is not load-bearing here — we only assert no error and that
	// the call is honored, replacing the previous dead `_ = id` discard.)
	if _, err := res.LastInsertId(); err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	var sid string
	var subjectDeleted, isPublished int64
	if err := db.QueryRow(`SELECT id, subject_deleted, is_published FROM comments WHERE id = ?`, "abc").
		Scan(&sid, &subjectDeleted, &isPublished); err != nil {
		t.Fatalf("select: %v", err)
	}
	if sid != "abc" {
		t.Errorf("id: want %q, got %q", "abc", sid)
	}
	if subjectDeleted != 0 {
		t.Errorf("subject_deleted: want 0 (DEFAULT false), got %d", subjectDeleted)
	}
	if isPublished != 1 {
		t.Errorf("is_published: want 1 (DEFAULT true), got %d", isPublished)
	}
}

// TestDefaultScalarTypesBeforeNotNull covers the remaining literal default
// shapes the parser already supports: numeric, text, NULL, and float.
// (CURRENT_TIMESTAMP and friends are intentionally not asserted here — the
// bundled expression evaluator does not implement those keywords today.)
func TestDefaultScalarTypesBeforeNotNull(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE typed (
		id INTEGER PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 42,
		score REAL NOT NULL DEFAULT 1.5,
		label TEXT NOT NULL DEFAULT 'untagged',
		note TEXT DEFAULT NULL
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO typed (id) VALUES (1)`); err != nil {
		t.Fatalf("insert with omitted scalar defaults failed: %v", err)
	}

	var count int64
	var score float64
	var label string
	var note sql.NullString
	if err := db.QueryRow(`SELECT count, score, label, note FROM typed WHERE id = 1`).
		Scan(&count, &score, &label, &note); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count != 42 {
		t.Errorf("count: want 42 (DEFAULT), got %d", count)
	}
	if score != 1.5 {
		t.Errorf("score: want 1.5 (DEFAULT), got %v", score)
	}
	if label != "untagged" {
		t.Errorf("label: want %q (DEFAULT), got %q", "untagged", label)
	}
	if note.Valid {
		t.Errorf("note: want NULL (DEFAULT NULL), got %q", note.String)
	}
}

// TestExplicitNullStillFailsNotNull covers criterion 3: an explicitly
// supplied NULL must STILL fail a NOT NULL column even when the column has
// a default. Both the explicit-column-list form and the positional form
// are exercised because buildInsertRow has two separate branches.
func TestExplicitNullStillFailsNotNull(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE x (
		id INTEGER PRIMARY KEY,
		flag BOOLEAN NOT NULL DEFAULT false,
		label TEXT NOT NULL DEFAULT 'x'
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	cases := []struct {
		name string
		sql  string
	}{
		{
			name: "explicit column list with NULL",
			sql:  `INSERT INTO x (id, flag) VALUES (1, NULL)`,
		},
		{
			name: "explicit column list with NULL (middle column)",
			sql:  `INSERT INTO x (flag, id) VALUES (NULL, 2)`,
		},
		{
			name: "positional form with NULL",
			sql:  `INSERT INTO x VALUES (3, NULL, 'hello')`,
		},
		{
			name: "positional form with NULL on text column",
			sql:  `INSERT INTO x VALUES (4, 1, NULL)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(tc.sql)
			if err == nil {
				t.Fatalf("expected NOT NULL failure, got nil")
			}
			if !strings.Contains(err.Error(), "NOT NULL") {
				t.Fatalf("expected NOT NULL error, got: %v", err)
			}
		})
	}

	// Sanity: the table is still empty after all the failed inserts.
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM x`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("after failed inserts, row count: want 0, got %d", n)
	}

	// And the defaults still work when the column is genuinely omitted.
	if _, err := db.Exec(`INSERT INTO x (id) VALUES (99)`); err != nil {
		t.Fatalf("omitted-defaults insert failed: %v", err)
	}
}

// TestReturningObservesAppliedDefault covers criterion 4: INSERT ... RETURNING
// must surface the value produced by the declared DEFAULT.
func TestReturningObservesAppliedDefault(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE r (
		id INTEGER PRIMARY KEY,
		flag BOOLEAN NOT NULL DEFAULT true,
		label TEXT NOT NULL DEFAULT 'hello'
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	rows, err := db.Query(`INSERT INTO r (id) VALUES (7) RETURNING id, flag, label`)
	if err != nil {
		t.Fatalf("insert returning: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected one RETURNING row")
	}
	var id int64
	var flag int64
	var label string
	if err := rows.Scan(&id, &flag, &label); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if id != 7 {
		t.Errorf("id: want 7, got %d", id)
	}
	if flag != 1 {
		t.Errorf("flag: want 1 (DEFAULT true), got %d", flag)
	}
	if label != "hello" {
		t.Errorf("label: want %q (DEFAULT), got %q", "hello", label)
	}
	if rows.Next() {
		t.Error("expected exactly one RETURNING row")
	}
}

// TestDefaultMultiRowTransactional covers criterion 5: multi-row and
// transactional inserts must apply the declared DEFAULT on every row.
func TestDefaultMultiRowTransactional(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE m (
		id INTEGER PRIMARY KEY,
		flag BOOLEAN NOT NULL DEFAULT false,
		n INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Multi-row VALUES inside a transaction: every row omits the defaulted
	// NOT NULL columns.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO m (id) VALUES (1), (2), (3)`); err != nil {
		t.Fatalf("multi-row insert: %v", err)
	}
	// Mixed: one row supplies a non-default value for n while still
	// omitting flag.
	if _, err := tx.Exec(`INSERT INTO m (id, n) VALUES (4, 99)`); err != nil {
		t.Fatalf("partial insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Outside the transaction the rows should all be visible with their
	// defaults applied.
	rows, err := db.Query(`SELECT id, flag, n FROM m ORDER BY id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()

	type row struct {
		id   int64
		flag int64
		n    int64
	}
	want := []row{
		{1, 0, 0},
		{2, 0, 0},
		{3, 0, 0},
		{4, 0, 99},
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.flag, &r.n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != len(want) {
		t.Fatalf("row count: want %d, got %d (%+v)", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("row %d: want %+v, got %+v", i, w, got[i])
		}
	}
}

// TestBooleanLiteralsAsExpressions confirms the root-cause fix generalizes:
// bare TRUE/FALSE are usable as integer literals (1/0) anywhere an
// expression is accepted, matching SQLite 3.23+.
func TestBooleanLiteralsAsExpressions(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE b (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO b (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	cases := []struct {
		expr string
		want int64
	}{
		{"SELECT true FROM b", 1},
		{"SELECT false FROM b", 0},
		{"SELECT TRUE FROM b", 1},
		{"SELECT FALSE FROM b", 0},
		{"SELECT true + 1 FROM b", 2},
		{"SELECT (true = 1) FROM b", 1},
		{"SELECT (false = 0) FROM b", 1},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			var got int64
			if err := db.QueryRow(tc.expr).Scan(&got); err != nil {
				t.Fatalf("query %q: %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("query %q: want %d, got %d", tc.expr, tc.want, got)
			}
		})
	}
}
