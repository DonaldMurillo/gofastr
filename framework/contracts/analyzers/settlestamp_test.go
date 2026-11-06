package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1417 exists because battery/queue's lease-expiry dead-letter
// sweep set status='failed' and nothing else while every other status
// transition of the jobs table wrote updated_at (and Nack last_error):
// a dead-lettered job then listed with its claim's timestamp and an
// earlier attempt's error (04ac2e5e6). The fixture reduces DBQueue's
// transitions under other names.

func hits1417(ds []contracts.Diagnostic) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == contracts.RuleSettleStamp {
			out = append(out, d)
		}
	}
	return out
}

// ssQueue is the jobs store: Sprintf with the table through a quoting
// method, the DBQueue spelling. SWEEP is the transition under test.
const ssQueue = `package workq

import "fmt"

type DB interface{ Exec(string, ...any) }

type Queue struct {
	db    DB
	table string
}

func (q *Queue) qt() string { return quote(q.table) }

func quote(s string) string { return "\"" + s + "\"" }

func (q *Queue) claim(id, tok string) {
	q.db.Exec(fmt.Sprintf(` + "`" + `UPDATE %s SET status='claimed', claimed_at=$1, updated_at=$1, claim_token=$2 WHERE id = $3` + "`" + `, q.qt()), 1, tok, id)
}

func (q *Queue) ack(id string) {
	q.db.Exec(fmt.Sprintf(` + "`" + `UPDATE %s SET status='done', updated_at=$1 WHERE id = $2 AND status='claimed'` + "`" + `, q.qt()), 1, id)
}

func (q *Queue) nack(id, why string) {
	q.db.Exec(fmt.Sprintf(` + "`" + `UPDATE %s
		SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		updated_at = $1, last_error = CASE WHEN $2 = '' THEN last_error ELSE $2 END
		WHERE id = $3 AND status='claimed'` + "`" + `, q.qt()), 1, why, id)
}

func (q *Queue) heartbeat(id string) {
	q.db.Exec(fmt.Sprintf("UPDATE %s SET claimed_at=$1 WHERE id=$2", q.qt()), 1, id)
}

func (q *Queue) sweep(id string) {
	q.db.Exec(fmt.Sprintf(` + "`" + `SWEEP` + "`" + `, q.qt()), id)
}
`

func ssFixture(t *testing.T, sweep string, extra map[string]string) []contracts.Diagnostic {
	t.Helper()
	files := map[string]string{"battery/workq/queue.go": strings.Replace(ssQueue, "SWEEP", sweep, 1)}
	for k, v := range extra {
		files[k] = v
	}
	return hits1417(fixture(t, files))
}

// 04ac2e5e6's pre-fix sweep fires, on the UPDATE's own line, naming the
// stamps its siblings write; the fix is quiet.
func TestSweepThatSkipsStampsFires(t *testing.T) {
	hs := ssFixture(t, `UPDATE %s SET status='failed'
		WHERE id=$1 AND status='claimed' AND attempts >= max_attempts`, nil)
	if len(hs) != 1 || hs[0].Line != 36 {
		t.Fatalf("want one finding on the sweep's UPDATE (line 36), got %v", hs)
	}
	if !strings.Contains(hs[0].Message, "updated_at") || !strings.Contains(hs[0].Message, "last_error") || !strings.Contains(hs[0].Message, "3 sibling") {
		t.Errorf("message should name the siblings' stamps and count: %s", hs[0].Message)
	}

	if hs := ssFixture(t, `UPDATE %s SET status='failed', updated_at=$1, last_error=$2
		WHERE id=$3 AND status='claimed' AND attempts >= max_attempts`, nil); len(hs) != 0 {
		t.Fatalf("the fixed sweep writes the stamps; want quiet, got %v", hs)
	}
}

// Silent postures and near-misses.
func TestSettleStampQuietPostures(t *testing.T) {
	// A non-status UPDATE (the heartbeat) never reports, though it writes
	// no stamp: it is not a transition.
	if hs := ssFixture(t, `UPDATE %s SET claimed_at=$1 WHERE id=$2`, nil); len(hs) != 0 {
		t.Fatalf("heartbeat-shaped sweep sets no status; want quiet, got %v", hs)
	}
	// A column whose name merely ends in "status" is not status.
	if hs := ssFixture(t, `UPDATE %s SET lease_status='x' WHERE id=$1`, nil); len(hs) != 0 {
		t.Fatalf("lease_status is not status; want quiet, got %v", hs)
	}
	// A stamp column on the status UPDATE of ANOTHER table: the sweep's
	// own table is a different literal, with no siblings.
	if hs := ssFixture(t, `UPDATE audit_rows SET status='failed' WHERE id=$1`, nil); len(hs) != 0 {
		t.Fatalf("a different table has no siblings; want quiet, got %v", hs)
	}
	// An upsert arm is not a transition.
	if hs := ssFixture(t, `INSERT INTO %s (id, status) VALUES ($1, 'pending') ON CONFLICT (id) DO UPDATE SET status = excluded.status`, nil); len(hs) != 0 {
		t.Fatalf("ON CONFLICT DO UPDATE is an upsert; want quiet, got %v", hs)
	}
	// A status written to the value the WHERE already requires is no
	// transition (the outbox's no-handler re-lease of a pending row).
	if hs := ssFixture(t, `UPDATE %s SET status='claimed', claim_token=$1
		WHERE id=$2 AND status='claimed'`, nil); len(hs) != 0 {
		t.Fatalf("status='claimed' on a claimed row moves nothing; want quiet, got %v", hs)
	}
	// …but a different literal in the WHERE is one.
	if hs := ssFixture(t, `UPDATE %s SET status='pending', claim_token=$1
		WHERE id=$2 AND status='claimed'`, nil); len(hs) != 1 {
		t.Fatalf("claimed → pending is a transition; want one finding, got %v", hs)
	}
	// A WHERE that mentions updated_at writes nothing: the SET list is
	// what counts.
	hs := ssFixture(t, `UPDATE %s SET status='failed' WHERE id=$1 AND updated_at < $2`, nil)
	if len(hs) != 1 {
		t.Fatalf("updated_at in the WHERE is not a write; want one finding, got %v", hs)
	}
}

// Quoted column names: a Set that spells its stamps with identifier
// quotes writes them the same. Cutting the statement at the first
// quote read the SET list as {status} alone — a false finding — and a
// quoted status column was not tracked at all.
func TestSettleStampQuotedColumns(t *testing.T) {
	// The stamps are written under quotes: quiet, like the unquoted fix.
	if hs := ssFixture(t, `UPDATE %s SET status='failed', "last_error"=$2, "updated_at"=now() WHERE id=$3`, nil); len(hs) != 0 {
		t.Fatalf("quoted stamp columns are writes; want quiet, got %v", hs)
	}
	// A quoted status column is still a status transition: it skips the
	// stamps, so it fires.
	if hs := ssFixture(t, `UPDATE %s SET "status"='failed' WHERE id=$1`, nil); len(hs) != 1 {
		t.Fatalf("a quoted status column skipping the stamps; want one finding, got %v", hs)
	}
}

// No convention, no finding: when one sibling writes no stamp either,
// the table has no rule to break.
func TestSettleStampNeedsAConvention(t *testing.T) {
	src := `package plain

import "database/sql"

func a(db *sql.DB) { db.Exec("UPDATE tasks SET status='a', updated_at=$1 WHERE id=$2") }
func b(db *sql.DB) { db.Exec("UPDATE tasks SET status='b' WHERE id=$1") }
func c(db *sql.DB) { db.Exec("UPDATE tasks SET status='c' WHERE id=$1") }
`
	if hs := hits1417(fixture(t, map[string]string{"framework/plain/plain.go": src})); len(hs) != 0 {
		t.Fatalf("two of three transitions write no stamp; want quiet, got %v", hs)
	}
	// One sibling is not a convention either.
	src = `package plain

import "database/sql"

func a(db *sql.DB) { db.Exec("UPDATE tasks SET status='a', updated_at=$1 WHERE id=$2") }
func b(db *sql.DB) { db.Exec("UPDATE tasks SET status='b' WHERE id=$1") }
`
	if hs := hits1417(fixture(t, map[string]string{"framework/plain/plain.go": src})); len(hs) != 0 {
		t.Fatalf("one sibling is not a convention; want quiet, got %v", hs)
	}
	// Different receivers' fields are different tables, even through the
	// same quoting helper.
	src = `package two

import "fmt"

type DB interface{ Exec(string, ...any) }
type Jobs struct{ db DB; table string }
type Runs struct{ db DB; table string }

func (j *Jobs) a() { j.db.Exec(fmt.Sprintf("UPDATE %s SET status='a', updated_at=$1", j.table)) }
func (j *Jobs) b() { j.db.Exec(fmt.Sprintf("UPDATE %s SET status='b', updated_at=$1", j.table)) }
func (r *Runs) c() { r.db.Exec(fmt.Sprintf("UPDATE %s SET status='c'", r.table)) }
`
	if hs := hits1417(fixture(t, map[string]string{"framework/two/two.go": src})); len(hs) != 0 {
		t.Fatalf("Runs.table is not Jobs.table; want quiet, got %v", hs)
	}
}

// Several statements in one literal (a migration script, or SQL quoted
// inside Go source text): each statement's SET and WHERE end where it
// does. Read past its end, the stampless UPDATE's WHERE would borrow the
// next statement's status='failed' and look like a no-op re-write.
func TestSettleStampStatementsInOneLiteral(t *testing.T) {
	src := "package script\n\nconst Script = `" + `
UPDATE runs SET status='done', updated_at=now() WHERE id=1;
UPDATE runs SET status='failed', last_error='x' WHERE id=2;
UPDATE runs SET status='failed' WHERE id=3
UPDATE runs SET status='failed', updated_at=now() WHERE status='failed'
` + "`\n"
	hs := hits1417(fixture(t, map[string]string{"framework/script/script.go": src}))
	if len(hs) != 1 || !strings.Contains(hs[0].Message, "2 sibling") || hs[0].Line != 6 {
		t.Fatalf("want the stampless third statement (line 6) against its 2 transition siblings, got %v", hs)
	}
}
