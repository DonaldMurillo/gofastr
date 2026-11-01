package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1409 exists because the v0.66 claim-token fencing of Ack/Nack
// in battery/queue missed release (probe TestDBReleaseCannotTouchReclaimant,
// 2026-09-04 red round): a stale worker released the re-claimant's claimed
// row back to pending by bare id and a third worker ran the handler
// concurrently. The fixtures reduce db.go's Ack fence + unfenced release
// to the shape, carry the fenced release as the fix posture, and add claim
// tables no repo site ever spelled.

// The repo site, reduced: the Ack DELETE fences on claim_token, release
// unfences by bare id. The finding lands on release's UPDATE, the
// statement that lacks the fence — not on the fenced sibling that arms
// the rule.
func TestUnfencedReleaseIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dbq.go": `package queue

import "database/sql"

func (q *DBQueue) Ack(db *sql.DB, table, id, token string) error {
	_, err := db.Exec(` + "`" + `DELETE FROM %s WHERE id = $1 AND status='claimed' AND claim_token = $2` + "`" + `, table, id, token)
	return err
}

func (q *DBQueue) release(db *sql.DB, table, id, now string) error {
	_, err := db.Exec(` + "`" + `UPDATE %s SET status='pending',
		attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
		scheduled_at = $1
		WHERE id = $2` + "`" + `, table, now, id)
	return err
}
`,
	})
	d := assertHas(t, ds, contracts.RuleUnfencedClaim)
	if d.Line != 11 {
		t.Errorf("finding on line %d, want 11 (release's UPDATE, not the fenced Ack): %s", d.Line, d.Location())
	}
}

// The fix posture: release fences on the token Dequeue minted, exactly
// like Ack, so a stale claimant matches no row.
func TestFencedReleaseIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dbq.go": `package queue

import "database/sql"

func (q *DBQueue) Ack(db *sql.DB, table, id, token string) error {
	_, err := db.Exec(` + "`" + `DELETE FROM %s WHERE id = $1 AND status='claimed' AND claim_token = $2` + "`" + `, table, id, token)
	return err
}

func (q *DBQueue) release(db *sql.DB, table, id, token, now string) error {
	_, err := db.Exec(` + "`" + `UPDATE %s SET status='pending',
		attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
		scheduled_at = $1
		WHERE id = $2 AND claim_token = $3` + "`" + `, table, now, id, token)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleUnfencedClaim, "release carries the same claim_token fence as Ack")
}

// A positive with no counterpart in this repo: a lease table fenced
// with the lease_token spelling, and an unfenced write whose only claim
// vocabulary is the status='claimed' predicate in its WHERE.
func TestUnfencedClaimFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"shards.go": `package shard

import "database/sql"

func complete(db *sql.DB, shard, id, tok string) error {
	_, err := db.Exec("DELETE FROM %s WHERE id = $1 AND status='claimed' AND lease_token = $2", shard, id, tok)
	return err
}

func heartbeat(db *sql.DB, shard, id, at string) error {
	_, err := db.Exec("UPDATE %s SET touched_at = $1 WHERE id = $2 AND status='claimed'", shard, at, id)
	return err
}
`,
	})
	d := assertHas(t, ds, contracts.RuleUnfencedClaim)
	if d.Line != 11 {
		t.Errorf("finding on line %d, want 11 (the unfenced heartbeat): %s", d.Line, d.Location())
	}
}

// The four postures that prove a claim-state write cannot touch a live
// claim, each pinned: the claim's own minting SET, the staleness bound
// on the claim timestamp, the terminal status guard, and the token
// predicate spelled with owner_token.
func TestUnfencedClaimFencedPosturesAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"postures.go": `package pool

import "database/sql"

func ack(db *sql.DB, t, id, tok string) error {
	_, err := db.Exec("DELETE FROM %s WHERE id = $1 AND status='claimed' AND owner_token = $2", t, id, tok)
	return err
}

func claim(db *sql.DB, t, id, at, tok string) error {
	_, err := db.Exec("UPDATE %s SET status='claimed', claimed_at=$1, owner_token=$2, attempts = attempts + 1 WHERE id = $3", t, at, tok, id)
	return err
}

func sweep(db *sql.DB, t, id, cutoff string) error {
	_, err := db.Exec("UPDATE %s SET status='failed' WHERE id=$1 AND status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $2 AND attempts >= max_attempts", t, id, cutoff)
	return err
}

func replay(db *sql.DB, t, id, now string) error {
	_, err := db.Exec("UPDATE %s SET status='pending', attempts=0, scheduled_at=$1 WHERE id=$2 AND status='failed'", t, now, id)
	return err
}

func nack(db *sql.DB, t, id, tok string) error {
	_, err := db.Exec("UPDATE %s SET status='pending' WHERE id = $1 AND owner_token = $2", t, id, tok)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleUnfencedClaim, "minter, staleness bound, terminal guard, and token predicate are the four fences")
}

// Without a token-fenced sibling in the file, the same unfenced write
// is a lease-shaped design (framework/outbox), not a missed fence: the
// rule does not arm.
func TestUnfencedClaimNeedsAFencedSibling(t *testing.T) {
	ds := fixture(t, map[string]string{
		"lease.go": `package outbox

import "database/sql"

func settle(db *sql.DB, t, rowID, consumer string, n int) error {
	_, err := db.Exec(` + "`" + `UPDATE %s
		SET status='pending', attempts=$1, next_attempt_at=now()
		WHERE row_id=$2 AND consumer=$3 AND status='pending'` + "`" + `, t, n, rowID, consumer)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleUnfencedClaim, "no token-fenced sibling arms the rule")
}

// A write with no claim vocabulary is not a claim-state write: the
// fence on the table does not make every column a claim column.
func TestUnfencedClaimIgnoresPlainWrites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"plain.go": `package queue

import "database/sql"

func ack(db *sql.DB, t, id, tok string) error {
	_, err := db.Exec("DELETE FROM %s WHERE id = $1 AND status='claimed' AND claim_token = $2", t, id, tok)
	return err
}

func rename(db *sql.DB, t, id, name string) error {
	_, err := db.Exec("UPDATE %s SET display_name = $1 WHERE id = $2", t, name, id)
	return err
}
`,
	})
	assertNot(t, ds, contracts.RuleUnfencedClaim, "display_name carries no claim semantics")
}

// claim_token IS NOT NULL proves somebody owns the row, not that the
// caller does: it is no fence, and it does not arm the rule either.
func TestUnfencedClaimNotNullIsNoFence(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dbq.go": `package queue

func q() []string {
	return []string{
		"DELETE FROM jobs WHERE id = $1 AND claim_token = $2",
		"UPDATE jobs SET status='pending', claimed_at = NULL WHERE id = $1 AND claim_token IS NOT NULL",
	}
}
`,
		"other/lone.go": `package other

const release = "UPDATE jobs SET status='pending', claimed_at = NULL WHERE id = $1 AND claim_token IS NOT NULL"
`,
	})
	got := countRule(t, ds, contracts.RuleUnfencedClaim)
	if len(got) != 1 || got[0].File != "dbq.go" {
		t.Fatalf("want 1 finding on dbq.go's IS NOT NULL release (the lone file is unarmed), got %v", got)
	}
}

// The fence lives in one file and the unfenced release in a sibling
// file of the same package: the package is armed, not the file. An
// upsert's DO UPDATE arm is an INSERT, outside the rule.
func TestUnfencedClaimArmsPerPackage(t *testing.T) {
	ds := fixture(t, map[string]string{
		"queue/ack.go": `package queue

const ackSQL = "DELETE FROM jobs WHERE id = $1 AND status='claimed' AND claim_token = $2"
`,
		"queue/release.go": `package queue

const releaseSQL = "UPDATE jobs SET status='pending', claimed_at = NULL WHERE id = $1"
`,
		"queue/schedule.go": `package queue

const upsertSQL = "INSERT INTO schedules (id, max_attempts) VALUES ($1, $2) ON CONFLICT(id) DO UPDATE SET max_attempts = excluded.max_attempts"
`,
		"other/release.go": `package other

const releaseSQL = "UPDATE jobs SET status='pending', claimed_at = NULL WHERE id = $1"
`,
	})
	got := countRule(t, ds, contracts.RuleUnfencedClaim)
	if len(got) != 1 || got[0].File != "queue/release.go" {
		t.Fatalf("want 1 finding on queue/release.go (other/ has no fenced sibling), got %v", got)
	}
}

// A lease bound spelled with the clock on the left (now() > lease_until)
// is the same staleness fence, cancelled/canceled/expired are terminal
// statuses no live claim holds, and claim_token IS NULL selects rows
// nobody holds.
func TestUnfencedClaimReversedAndTerminalQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"pool.go": `package pool

func q() []string {
	return []string{
		"DELETE FROM jobs WHERE id = $1 AND claim_token = $2",
		"UPDATE jobs SET status='pending', claim_token = NULL WHERE id = $1 AND now() > lease_until",
		"UPDATE jobs SET status='failed' WHERE id = $1 AND $2 >= claimed_at AND attempts >= max_attempts",
		"DELETE FROM jobs WHERE status = 'cancelled' AND claimed_at IS NOT NULL",
		"DELETE FROM jobs WHERE status = 'canceled' AND attempts > 0",
		"UPDATE jobs SET attempts = 0 WHERE id = $1 AND status = 'expired'",
		"UPDATE jobs SET claimed_at = NULL WHERE id = $1 AND claim_token IS NULL",
	}
}
`,
	})
	assertNot(t, ds, contracts.RuleUnfencedClaim,
		"a reversed staleness bound and a cancelled/expired status cannot touch a live claim")
}

// Clearing the token (to NULL or the empty string) is not the claim
// minting its fence: a release that clears it by bare id hands the
// re-claimant's live row back to pending.
func TestUnfencedClaimTokenClearIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"dbq.go": `package queue

func q() []string {
	return []string{
		"DELETE FROM jobs WHERE id = $1 AND claim_token = $2",
		"UPDATE jobs SET status='pending', claim_token = NULL WHERE id = $1",
		"UPDATE jobs SET status='pending', claim_token = '' WHERE id = $1",
	}
}
`,
	})
	if got := countRule(t, ds, contracts.RuleUnfencedClaim); len(got) != 2 {
		t.Fatalf("want 2 findings (NULL and '' token clears by bare id), got %d: %v", len(got), got)
	}
}
