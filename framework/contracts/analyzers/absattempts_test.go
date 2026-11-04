package analyzers_test

import (
	"os"
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

func TestCounterSQLLikeTextIsNotFormatVerb(t *testing.T) {
	source, err := os.ReadFile("testdata/review/counter-like.go")
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := fixture(t, map[string]string{
		"queue.go": string(source),
	})
	assertNot(t, diagnostics, contracts.RuleAbsoluteAttempts, "LIKE %active% is SQL text, not a fmt verb")
}

// A format held in a constant or local is still a format: Sprintf's
// first argument names it instead of spelling the literal inline.
func TestNamedSprintfFormatCountsVerbs(t *testing.T) {
	ds := fixture(t, map[string]string{
		"named.go": `package dispatch

import "database/sql"
import "fmt"

const bounceSQL = "UPDATE dispatch SET retries = %d WHERE run_id = 1"

func noteBounce(db *sql.DB, count int) error {
	_, err := db.Exec(fmt.Sprintf(bounceSQL, count+1))
	return err
}

func noteCrawl(db *sql.DB, count int) error {
	query := "UPDATE pages SET tries = %d WHERE page_id = 1"
	_, err := db.Exec(fmt.Sprintf(query, count))
	return err
}
`,
	})
	if got := len(countRule(t, ds, contracts.RuleAbsoluteAttempts)); got != 2 {
		t.Fatalf("want 2 findings for named formats, got %v", countRule(t, ds, contracts.RuleAbsoluteAttempts))
	}
}

// Two positives with no counterpart in this repo: a retry counter on a
// dispatch table spelled "retries" with a Sprintf %d, and a "tries"
// counter on a crawl table with the sqlite placeholder.
func TestAbsoluteAttemptsFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dispatch.go": `package dispatch

import "database/sql"
import "fmt"

func noteBounce(db *sql.DB, runID string, count int) error {
	_, err := db.Exec(fmt.Sprintf("UPDATE dispatch SET retries = %d WHERE run_id = %s", count+1, runID))
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

// A placeholder wrapped in a cast or a scalar function, and a quoted
// column name, are the same host snapshot written over the counter.
func TestAbsoluteAttemptsWrappedIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"settle.go": `package outbox

func q() []string {
	return []string{
		"UPDATE jobs SET attempts = CAST($1 AS INTEGER) WHERE id = $2",
		"UPDATE jobs SET attempts = COALESCE($1, 0), status = $2 WHERE id = $3",
		"UPDATE jobs SET status = $1, attempts = LEAST($2, max_attempts) WHERE id = $3",
		` + "`" + `UPDATE jobs SET "attempts" = $1 WHERE id = $2` + "`" + `,
		"UPDATE jobs SET attempts = $1::int WHERE id = $2",
	}
}
`,
	})
	if got := countRule(t, ds, contracts.RuleAbsoluteAttempts); len(got) != 5 {
		t.Fatalf("want 5 findings (CAST, COALESCE, LEAST, quoted, ::int), got %d: %v", len(got), got)
	}
}

// A wrapper whose expression reads the counter itself is still the
// relative form: the column computes itself in SQL.
func TestAbsoluteAttemptsWrappedRelativeQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"claim.go": `package queue

func q() []string {
	return []string{
		"UPDATE jobs SET attempts = LEAST(attempts + 1, $1) WHERE id = $2",
		"UPDATE jobs SET attempts = COALESCE(attempts, 0) + 1, claimed_at = $1 WHERE id = $2",
		` + "`" + `UPDATE jobs SET "attempts" = "attempts" + 1, status = $1 WHERE id = $2` + "`" + `,
		"UPDATE jobs SET max_attempts = $1 WHERE id = $2",
	}
}
`,
	})
	assertNot(t, ds, contracts.RuleAbsoluteAttempts,
		"a counter expression that reads its own column is relative, and max_attempts is not the counter")
}

// A Sprintf verb with an argument index or a width is the same host
// snapshot: this repo's own SQL spells table and limit slots %[2]s and
// %[6]d (entityui), so the counter spelled the same way counts.
func TestAbsoluteAttemptsIndexedVerbIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"settle.go": `package outbox

import "fmt"

func q(table string, n, id int) string {
	return fmt.Sprintf("UPDATE %[1]s SET status=%[3]v, attempts=%[2]d WHERE id=%[4]d", table, n, "dead", id)
}
`,
	})
	assertHas(t, ds, contracts.RuleAbsoluteAttempts)
	ds = fixture(t, map[string]string{
		"settle.go": `package outbox

import "fmt"

func q(attempts int) string {
	return fmt.Sprintf("UPDATE jobs SET attempts=%02d WHERE id=1", attempts)
}
`,
	})
	assertHas(t, ds, contracts.RuleAbsoluteAttempts)
}

// A marker above a call whose multi-line SQL literal holds the finding:
// the comment cannot sit inside the raw string, so the waiver has to
// cover the whole literal that follows it. Without that, the 1408
// finding stays AND the marker is reported stale (GOFASTR0002).
func TestAbsoluteAttemptsMarkerCoversLiteral(t *testing.T) {
	ds := fixture(t, map[string]string{
		"settle.go": `package outbox

import "database/sql"

func markDead(db *sql.DB, table string, n int, rowID string) error {
	//gofastr:allow(GOFASTR1408) the settle path owns the row exclusively; the count is per-runner by design
	_, err := db.Exec(` + "`" + `UPDATE %s
		SET status='dead', attempts=$1, claimed_until=NULL
		WHERE row_id=$2` + "`" + `, table, n, rowID)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleAbsoluteAttempts, "the marker above the call waives the finding inside its literal")
	assertNot(t, ds, contracts.RuleSuppressionStale, "the marker matched the finding inside the literal, so it is not stale")
}
