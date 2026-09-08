package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1414 exists because framework/outbox claimDeliveriesSQLite
// shipped without the `next_attempt_at IS NULL OR next_attempt_at
// <= $` backoff predicate its Postgres twin claimDeliveriesPostgres
// has (2026-09-07 adversarial round): the two dialect paths selected
// different rows, and only the SQLite one re-claimed deliveries past
// their retry deadline. Fixtures reduce the outbox pair, the webhook
// pair whose predicates match after normalization, and both branch
// spellings of heuristic (b).

// The outbox oracle, reduced: the SQLite claim lacks the backoff atom.
func TestDialectDriftMissingPredicateIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"delivery.go": `package outbox

import (
	"context"
	"database/sql"
)

func (o *Outbox) claimDeliveriesPostgres(ctx context.Context, db *sql.DB) error {
	q := ` + "`" + `SELECT d.row_id, d.consumer FROM deliveries d
		WHERE d.status = 'pending'
		  AND (d.claimed_until IS NULL OR d.claimed_until <= $2)
		  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $2)
		ORDER BY d.created_at LIMIT $3` + "`" + `
	_, err := db.Query(q)
	return err
}

func (o *Outbox) claimDeliveriesSQLite(ctx context.Context, db *sql.DB) error {
	pick := ` + "`" + `SELECT d.row_id, d.consumer FROM deliveries d
		WHERE d.status = 'pending'
		  AND (d.claimed_until IS NULL OR d.claimed_until <= ?)
		ORDER BY d.created_at LIMIT ?` + "`" + `
	_, err := db.Query(pick)
	return err
}
`,
	})
	found := countRule(t, ds, contracts.RuleDialectDrift)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 finding (on the SQLite side), got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "next_attempt_at") {
		t.Errorf("finding must name the missing predicate: %q", found[0].Message)
	}
	if !strings.Contains(found[0].Message, "claimDeliveriesSQLite") {
		t.Errorf("finding must name the poorer query: %q", found[0].Message)
	}
}

// The webhook claim pair, reduced: $n vs ? with identical predicates
// is the normal twin, and the SQLite write-back UPDATE by id is not a
// selection predicate — nothing fires.
func TestDialectDriftEquivalentTwinsAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"sql.go": `package webhook

import (
	"context"
	"database/sql"
	"strings"
)

func (s *SQLStore) claimPostgres(ctx context.Context, db *sql.DB) error {
	q := ` + "`" + `UPDATE deliveries SET attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM deliveries
			WHERE status = $2 AND next_attempt_at <= $3
			ORDER BY next_attempt_at LIMIT 5 FOR UPDATE SKIP LOCKED
		) RETURNING id` + "`" + `
	_, err := db.Query(q)
	return err
}

func (s *SQLStore) claimSqlite(ctx context.Context, tx *sql.Tx) error {
	selQ := ` + "`" + `SELECT id FROM deliveries
		WHERE status = ? AND next_attempt_at <= ?
		ORDER BY next_attempt_at LIMIT 5` + "`" + `
	if _, err := tx.Query(selQ); err != nil {
		return err
	}
	ids := []string{"a", "b"}
	ph := make([]string, len(ids))
	for i := range ids {
		ph[i] = "?"
	}
	updQ := ` + "`" + `UPDATE deliveries SET attempts = attempts + 1 WHERE id IN (` + "`" + ` + strings.Join(ph, ",") + ` + "`" + `)` + "`" + `
	_, err := tx.Exec(updQ)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleDialectDrift,
		"placeholder spellings and the write-back UPDATE are not predicate drift")
}

// Heuristic (b): the dialect if with SQL in each arm, in the
// early-return spelling deliveryUpdate uses.
func TestDialectDriftBranchArmsAreCompared(t *testing.T) {
	ds := fixture(t, map[string]string{
		"branch.go": `package store

import (
	"database/sql"
	"fmt"
)

type Store struct{ dialect string }

func (s *Store) settle(db *sql.DB, id string) error {
	if s.dialect == "postgres" {
		_, err := db.Exec(fmt.Sprintf(` + "`" + `UPDATE deliveries SET status = 'done'
			WHERE id = $1 AND (attempts < 5 OR attempts IS NULL)` + "`" + `), id)
		return err
	}
	_, err := db.Exec(` + "`" + `UPDATE deliveries SET status = 'done' WHERE id = ?` + "`" + `, id)
	return err
}

func (s *Store) subUpsert(db *sql.DB) error {
	if s.dialect == "postgres" {
		_, err := db.Exec(` + "`" + `INSERT INTO subs (id, url) VALUES ($1, $2)
			ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url` + "`" + `)
		return err
	}
	_, err := db.Exec(` + "`" + `INSERT INTO subs (id, url) VALUES (?, ?)
		ON CONFLICT (id) DO UPDATE SET url = excluded.url` + "`" + `)
	return err
}
`,
	})
	found := countRule(t, ds, contracts.RuleDialectDrift)
	if len(found) != 1 {
		t.Fatalf("want 1 finding (the divergent settle arms; the upsert has no WHERE), got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "attempts") {
		t.Errorf("finding must name the missing predicate: %q", found[0].Message)
	}
}

// The switch spelling (framework/migrate's DialectPostgres /
// DialectSQLite case labels) pairs the same way.
func TestDialectDriftSwitchCasesAreCompared(t *testing.T) {
	ds := fixture(t, map[string]string{
		"switch.go": `package migrate

import "database/sql"

type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

func prune(db *sql.DB, d Dialect, cutoff string) error {
	switch d {
	case DialectPostgres:
		_, err := db.Exec(` + "`" + `DELETE FROM occurrences
			WHERE schedule_id = $1 AND status = 'skipped' AND created_at < $2` + "`" + `, "s", cutoff)
		return err
	case DialectSQLite:
		_, err := db.Exec(` + "`" + `DELETE FROM occurrences
			WHERE schedule_id = ? AND created_at < ?` + "`" + `, "s", cutoff)
		return err
	}
	return nil
}
`,
	})
	found := countRule(t, ds, contracts.RuleDialectDrift)
	if len(found) != 1 {
		t.Fatalf("want 1 finding (the SQLite arm lacks status='skipped'), got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "status = 'skipped'") {
		t.Errorf("finding must name the missing predicate: %q", found[0].Message)
	}
}

// The different-catalog posture, reduced from framework/migrate
// tableExistsBulkPostgres / tableExistsBulkSQLite and
// framework/export_data tableExists: each twin queries its own
// dialect's system catalog (pg_tables / information_schema.tables vs
// sqlite_master), so the predicates cannot match textually and no
// parity is owed. The rule extracts each side's table set and stops
// before comparing.
func TestDialectDriftDifferentCatalogsAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"bulk.go": `package migrate

import (
	"context"
	"database/sql"
)

func tableExistsBulkPostgres(ctx context.Context, db *sql.DB) error {
	q := "SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND lower(tablename) IN ($1)"
	_, err := db.Query(q)
	return err
}

func tableExistsBulkSQLite(ctx context.Context, db *sql.DB) error {
	q := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	_, err := db.Query(q)
	return err
}
`,
		"export.go": `package export

import (
	"database/sql"
)

func tableExists(db *sql.DB, dialect string, table string) error {
	if dialect == "postgres" {
		var ok bool
		err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&ok)
		return err
	}
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = $1", table).Scan(&name)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleDialectDrift,
		"twins over different system catalogs owe no predicate parity")
}

// The same-table subset posture, the outbox shape: the Postgres twin
// is one atomic UPDATE over the SAME table the SQLite twin selects
// from first, so its table set is a superset and the atoms still
// compare — the missing backoff predicate stays a finding.
func TestDialectDriftSameTableSubsetIsCompared(t *testing.T) {
	ds := fixture(t, map[string]string{
		"delivery.go": `package outbox

import (
	"database/sql"
	"fmt"
)

func claimPostgres(db *sql.DB) error {
	q := fmt.Sprintf(` + "`" + `UPDATE %s SET claimed_until = $1, attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM %s
			WHERE status = 'pending' AND (claimed_until IS NULL OR claimed_until <= $2)
			  AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
			LIMIT $3)` + "`" + `, "deliveries", "deliveries")
	_, err := db.Query(q)
	return err
}

func claimSqlite(db *sql.DB) error {
	pick := fmt.Sprintf(` + "`" + `SELECT id FROM %s
		WHERE status = 'pending' AND (claimed_until IS NULL OR claimed_until <= ?)
		LIMIT ?` + "`" + `, "deliveries")
	_, err := db.Query(pick)
	return err
}
`,
	})
	found := countRule(t, ds, contracts.RuleDialectDrift)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 finding on the SQLite side, got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "next_attempt_at") {
		t.Errorf("finding must name the missing predicate: %q", found[0].Message)
	}
}
