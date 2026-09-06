package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1408 exists because the outbox and webhook settle paths wrote
// the attempts counter from a host-side snapshot (probes
// TestFailureSettleAttemptsMonotonic, TestSettleAttemptsCountEveryPost,
// TestClaimConsumesAttemptCrashLoop, 2026-09-04 red round): two
// overlapping runners both read N and both write N+1, and a crash
// between claim and settle consumed nothing. Fixtures reduce the three
// real sites (outbox mark-dead, webhook postgres settle, webhook sqlite
// settle, inbound envelope update) to their shape, carry the
// battery/queue relative claim as the negative, and add positives no
// repo site ever spelled.

// The outbox settle, reduced: the dead-letter UPDATE writes attempts=$1
// from a snapshot computed before the write.
func TestAbsoluteAttemptsSettleIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"settle.go": `package outbox

import "database/sql"

func (o *Outbox) markDead(db *sql.DB, table string, newAttempts int, rowID, consumer string) error {
	_, err := db.Exec(` + "`" + `UPDATE %s
		SET status='dead', attempts=$1, last_error=$2, claimed_until=NULL
		WHERE row_id=$3 AND consumer=$4 AND status='pending'` + "`" + `,
		table, newAttempts, "boom", rowID, consumer)
	return err
}
`,
	})
	d := assertHas(t, ds, contracts.RuleAbsoluteAttempts)
	if d.Line != 7 {
		t.Errorf("finding on line %d, want 7 (the attempts=$1 line, not the UPDATE line): %s", d.Line, d.Location())
	}
}

// The webhook store's dialect pair, reduced: both spellings of the
// absolute counter (postgres $n and sqlite ?) in one file, both
// reported.
func TestAbsoluteAttemptsBothDialectsAreReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"store.go": `package webhook

import "database/sql"

func (s *Store) deliveryUpdate(db *sql.DB, table string) string {
	if s.dialect == "postgres" {
		return "UPDATE %s SET attempts = $1, status = $2, last_error = $3 WHERE id = $4"
	}
	return "UPDATE %s SET attempts = ?, status = ?, last_error = ? WHERE id = ?"
}
`,
	})
	if got := len(countRule(t, ds, contracts.RuleAbsoluteAttempts)); got != 2 {
		t.Fatalf("want 2 findings (postgres $1 and sqlite ?), got %d: %v", got, countRule(t, ds, contracts.RuleAbsoluteAttempts))
	}
}

// The battery/queue claim, reduced: the counter increments itself in
// SQL, so overlapping claimants cannot under-count. This is the fix
// posture the whole rule points at.
func TestRelativeClaimCounterIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"claim.go": `package queue

import "database/sql"

func (q *DBQueue) claim(db *sql.DB, table string, now, token string) error {
	_, err := db.Exec(` + "`" + `UPDATE %s SET status='claimed', claimed_at=$1, claim_token=$2, attempts = attempts + 1
		WHERE id = (SELECT id FROM %s WHERE status='pending' LIMIT 1)` + "`" + `, table, now, token, table)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleAbsoluteAttempts, "attempts = attempts + 1 is the relative form the rule demands")
}

// Two positives with no counterpart in this repo: a retry counter on a
// dispatch table spelled "retries" with a Sprintf %d, and a "tries"
// counter on a crawl table with the sqlite placeholder.
func TestAbsoluteAttemptsFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dispatch.go": `package dispatch

import "database/sql"

func noteBounce(db *sql.DB, runID string, count int) error {
	_, err := db.Exec("UPDATE dispatch SET retries = %d WHERE run_id = %s", count+1, runID)
	return err
}
`,
		"crawl.go": `package crawl

import "database/sql"

func recordAttempt(db *sql.DB, pageID string) error {
	_, err := db.Exec("UPDATE pages SET tries = ? WHERE page_id = ?", nextPage(pageID))
	return err
}
`,
	})
	if got := len(countRule(t, ds, contracts.RuleAbsoluteAttempts)); got != 2 {
		t.Fatalf("want 2 findings for retries/tries spellings, got %v", countRule(t, ds, contracts.RuleAbsoluteAttempts))
	}
}

// The documented silences: the literal reset (a terminal-status replay
// writing the same zero every concurrent writer would write), the
// counter as a WHERE predicate (it selects rows, it writes nothing),
// the INSERT column list (placeholders there are values for a new row,
// not a count overwrite), and the relative CASE decrement.
func TestAbsoluteAttemptsStaysSilent(t *testing.T) {
	ds := fixture(t, map[string]string{
		"quiet.go": `package queue

import "database/sql"

func replay(db *sql.DB, table, id string, now string) error {
	_, err := db.Exec("UPDATE %s SET status='pending', attempts=0, scheduled_at=$1 WHERE id=$2 AND status='failed'", table, now, id)
	return err
}

func claimCount(db *sql.DB, table string) error {
	_, err := db.Exec("SELECT id FROM %s WHERE attempts = $1 AND status='claimed'", table, 3)
	return err
}

func insert(db *sql.DB, table string) error {
	_, err := db.Exec("INSERT INTO %s (id, attempts, status) VALUES ($1, $2, $3)", table, "j1", 0, "pending")
	return err
}

func giveBack(db *sql.DB, table, id string) error {
	_, err := db.Exec("UPDATE %s SET attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END WHERE id = $1", table, id)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleAbsoluteAttempts, "literal reset, WHERE predicate, INSERT values, and relative CASE are all documented silences")
}

// Commented-out SQL is not SQL: the extractor reads literals through
// the AST, so prose in a comment cannot produce a statement.
func TestAbsoluteAttemptsIgnoresCommentedOutSQL(t *testing.T) {
	ds := fixture(t, map[string]string{
		"notes.go": `package queue

// The old spelling, kept for the migration note:
//   UPDATE jobs SET attempts = $1 WHERE id = $2
func migrate() {}
`,
	})
	assertNot(t, ds, contracts.RuleAbsoluteAttempts, "a comment is not a string literal")
}
