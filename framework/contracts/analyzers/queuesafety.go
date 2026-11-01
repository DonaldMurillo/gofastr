package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// The two queue-safety SQL rules. Both read whole string literals through
// the AST (so comments and prose cannot produce statements) and both key
// on the SET/WHERE clause structure rather than bare substring presence:
// a counter comparison in a WHERE is a predicate, not a write, and a
// token column in a SET is the claim MINTING its fence, not matching one.

// ----------------------------------------------------------------------
// GOFASTR1408: absolute retry-counter writes in UPDATE SET.
// ----------------------------------------------------------------------

// Bug class: an UPDATE whose SET assigns the retry/failure counter from a
// host value (attempts = $1 / ? / %s, bare or wrapped: CAST($1 AS
// INTEGER), COALESCE($1, 0), LEAST($1, max_attempts), $1::int, and
// the quoted "attempts" column) instead of the relative
// attempts = attempts + 1. The host value is computed in Go from a row
// read before the write, so two overlapping runners — a lease-expiry
// re-claimant and the stale worker it replaced, or a crash between claim
// and settle — both read N and both write N+1, and the row's attempt
// budget under-counts. Produced by the 2026-09-04 red-probe round:
// framework/outbox TestFailureSettleAttemptsMonotonic (the settle SQL
// wrote attempts=$1 from a pre-claim snapshot), battery/webhook
// TestSettleAttemptsCountEveryPost and TestClaimConsumesAttemptCrashLoop
// (the settle UPDATE carried the whole count, so a crash between claim
// and settle consumed nothing and the delivery re-ran outside its
// budget). The fix posture is battery/queue db.go's claim: the counter
// moves into the claim UPDATE as attempts = attempts + 1 and settle
// writes state, not arithmetic.
//
// Deliberately silent on:
//   - the relative spellings (attempts = attempts + 1, the CASE
//     decrement in battery/queue release, and any wrapper whose value
//     reads the counter itself, LEAST(attempts + 1, $1)): the column
//     computes itself in SQL, and no shared snapshot can under-count;
//   - literal constants (attempts = 0 on a reset/replay guarded by a
//     terminal status): the value is not a host snapshot, and every
//     concurrent writer writes the same zero;
//   - INSERT column lists and WHERE predicates (attempts = $1 inside a
//     WHERE selects rows, it does not write the counter): only the SET
//     clause of an UPDATE is read;
//   - _test.go and generated files (AppFiles already excludes both).
func ruleAbsoluteAttempts(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, stmt := range sqlStatements(file) {
		if stmt.verb != "update" {
			continue
		}
		for _, m := range reCounterAssign.FindAllStringSubmatchIndex(stmt.set, -1) {
			lo, hi := m[2], m[3] // the quoted spelling's group
			if lo < 0 {
				lo, hi = m[4], m[5] // the bare spelling's group
			}
			col := stmt.set[lo:hi]
			rhs := setValue(stmt.set[m[1]:])
			if !reHostValue.MatchString(rhs) || selfReference(rhs, col) {
				continue
			}
			d := diag(p, contracts.RuleAbsoluteAttempts, rel,
				stmt.at(stmt.setOff+m[0]),
				fmt.Sprintf("UPDATE SET writes the retry counter from a host value (%s), not the relative form: overlapping claimants each write back their own snapshot and under-count — increment in SQL (attempts = attempts + 1, the battery/queue claim spelling) or move the count out of the settle write",
					strings.TrimSpace(stmt.set[m[0]:m[1]+len(rhs)])))
			out = append(out, d)
		}
	}
	return out
}

var (
	// reCounterAssign matches a counter column, bare or double-quoted,
	// opening an assignment. The value that follows is read by
	// setValue, so CAST($1 …), COALESCE($1, 0), LEAST($1, …) and
	// $1::int are judged by what they compute, not by their first
	// token.
	reCounterAssign = regexp.MustCompile(`(?i)(?:"(attempts|retries|tries|failures)"|\b(attempts|retries|tries|failures)\b)\s*=`)
	// reHostValue matches a host value: a positional placeholder ($n,
	// the sqlite ?) or a Sprintf verb. \d+ consumes the whole number,
	// so $12 needs no lookahead of its own.
	reHostValue = regexp.MustCompile(`\$\d+|\?|%[sdv]`)
)

// setValue returns one SET assignment's value expression: the text up
// to the next comma outside parentheses, or the end of the clause.
func setValue(rest string) string {
	depth := 0
	for i, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth <= 0 {
				return rest[:i]
			}
		}
	}
	return rest
}

// selfReference reports whether a SET value reads the counter column it
// assigns (attempts = LEAST(attempts + 1, $1)): the column computes
// itself in SQL, the relative form. The match is a whole word, so
// max_attempts does not count as attempts.
func selfReference(value, col string) bool {
	value, col = asciicase.Lower(value), asciicase.Lower(col)
	for from := 0; ; {
		i := strings.Index(value[from:], col)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(col)
		if (start == 0 || !ecIdentByte(value[start-1])) && (end == len(value) || !ecIdentByte(value[end])) {
			return true
		}
		from = end
	}
}

// ----------------------------------------------------------------------
// GOFASTR1409: unfenced claim-state writes on a token-fenced table.
// ----------------------------------------------------------------------

// Bug class: a queue table whose completion paths are fenced on a
// per-claim token (claim_token = $n in the WHERE, minted by the claim
// UPDATE) carries another UPDATE/DELETE that touches claim state with no
// fence of its own. The lease-expiry clause in the claim deliberately
// re-claims rows whose worker may still be running, so a stale claimant
// is a designed state; its unfenced write by bare id then mutates the
// re-claimant's live row. Probe TestDBReleaseCannotTouchReclaimant
// (2026-09-04 red-probe round) pinned it in battery/queue db.go release:
// the v0.66 Ack/Nack claim fencing never reached release, so a stale
// gate-deferral released the re-claimant's claimed row back to pending
// and a third worker ran the handler concurrently.
//
// A statement reports when the file also holds a token-fenced sibling
// (proving the table is fenced) and the statement:
//   - touches claim vocabulary — claimed_at / a *_token column /
//     claimed_until / lease_until / max_attempts / attempts in SET or
//     WHERE, or a WHERE predicate on status='claimed' — and
//   - carries none of the postures that prove it cannot touch
//     someone's live claim:
//     a token predicate in the WHERE (claim_token = $n, the Ack/Nack
//     fence; claim_token IS NOT NULL is not one, it proves somebody
//     owns the row, not the caller),
//     an unowned-row predicate (claim_token IS NULL: a live claim
//     always carries its token),
//     the minting SET itself (SET claim_token = $n creates the
//     ownership the WHERE would otherwise have to match; clearing the
//     token to NULL or the empty string is a release, not a mint),
//     a staleness bound on the claim timestamp in either operand order
//     (claimed_at <= $n, now() > lease_until: a re-claim refreshes
//     claimed_at, so the bound can never match a live re-claimant —
//     the dead-letter sweep's fence),
//     a terminal status guard in the WHERE (status='failed'/'dead'/
//     'cancelled'/'expired'/…: a live claim holds status='claimed',
//     so the predicate selects rows nobody owns — Replay's fence).
//
// The rule arms per package directory: the fenced Ack and the release
// that misses the fence are often in sibling files.
//
// Deliberately silent on:
//   - packages with no token-fenced statement: the rule is about a table
//     whose completion paths established the fence and then missed one
//     writer; an unfenced table is a different (lease-shaped) design,
//     see framework/outbox;
//   - SELECTs, DDL, and INSERTs, an upsert's ON CONFLICT … DO UPDATE
//     arm included: they cannot retire or re-queue someone else's
//     claim;
//   - writes with no claim vocabulary (a status flip on a plain row): a
//     fence on the table does not make every column a claim column;
//   - _test.go and generated files (AppFiles already excludes both).
func ruleUnfencedClaim(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	armed, _ := p.Memo("unfencedclaim.package:"+dir, func() any {
		return unfencedClaimArmed(p, dir)
	}).(bool)
	if !armed {
		return nil
	}
	var out []contracts.Diagnostic
	for _, stmt := range sqlStatements(file) {
		switch {
		case stmt.upsert, // an INSERT's conflict arm writes the row it inserts
			reTokenFence.MatchString(stmt.where),
			reTokenUnowned.MatchString(stmt.where),
			reTokenMint.MatchString(stmt.set), // the claim mints its token
			reStaleClaimAt.MatchString(stmt.where),
			reStaleClaimAtRev.MatchString(stmt.where),
			reTerminalStatus.MatchString(stmt.where):
			continue
		}
		if !reClaimVocab.MatchString(stmt.set) &&
			!reClaimVocab.MatchString(stmt.where) &&
			!reClaimedStatus.MatchString(stmt.where) {
			continue
		}
		d := diag(p, contracts.RuleUnfencedClaim, rel, stmt.at(stmt.verbOff),
			fmt.Sprintf("%s touches claim state with no fence of its own (no token predicate, no claimed_at staleness bound, no terminal status guard): a stale claimant can mutate the re-claimant's live row — fence it on the claim token the way Ack/Nack do (battery/queue db.go)",
				strings.ToUpper(stmt.verb[:1])+stmt.verb[1:]))
		out = append(out, d)
	}
	return out
}

// unfencedClaimArmed reports whether any statement in the package
// directory fences on a claim token. The fence and the writer that
// misses it are often in sibling files (an ack.go and a release.go),
// so the arming is per package, the way GOFASTR1414/1415 memoize.
func unfencedClaimArmed(p *contracts.Pass, dir string) bool {
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for _, stmt := range sqlStatements(file) {
			if reTokenFence.MatchString(stmt.where) {
				return true
			}
		}
	}
	return false
}

var (
	// reTokenFence is the caller's own token matched against the row:
	// claim_token = $n (or ?, a Sprintf verb), or the null-safe
	// IS NOT DISTINCT FROM $n. claim_token IS NOT NULL is not one: it
	// proves somebody owns the row, not that the caller does.
	reTokenFence = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s*(?:=|IS\s+NOT\s+DISTINCT\s+FROM)\s*(?:\$\d+|\?|%[sdv])`)
	// reTokenUnowned selects rows nobody holds (claim_token IS NULL): a
	// live claim always carries its token, so it cannot match one.
	reTokenUnowned = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s+IS\s+NULL\b`)
	// reTokenMint is the claim minting its fence: the token column SET
	// from a host value. Clearing it (to NULL or the empty string) is a
	// release, and needs a fence like any other.
	reTokenMint    = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s*=\s*(?:\$\d+|\?|%[sdv])`)
	reStaleClaimAt = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|lease_until|lease_expires_at)\s*(?:<=|<)`)
	// reStaleClaimAtRev is the same bound with the clock on the left:
	// now() > lease_until, $1 >= claimed_at.
	reStaleClaimAtRev = regexp.MustCompile(`(?i)(?:>=|>)\s*(?:claimed_at|claimed_until|lease_until|lease_expires_at)\b`)
	reTerminalStatus  = regexp.MustCompile(`(?i)\bstatus\b[^;]*'(?:failed|dead|abandoned|dispatched|completed|done|succeeded|cancelled|canceled|expired)'`)
	reClaimedStatus   = regexp.MustCompile(`(?i)\bstatus\b[^;]*'claimed'`)
	reClaimVocab      = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|claim_token|lease_token|owner_token|lease_until|lease_expires_at|max_attempts|attempts)\b`)
)

// ----------------------------------------------------------------------
// shared: statements inside string literals
// ----------------------------------------------------------------------

// sqlStmt is one UPDATE or DELETE statement found inside a string
// literal, with its clause slices and the geometry needed to place a
// finding on the right source line.
type sqlStmt struct {
	verb    string // "update" / "delete", lowercased
	verbOff int    // offset of the verb within text
	text    string // the statement, from the unquoted literal value
	set     string // the SET clause ("" when absent)
	setOff  int    // offset of the SET clause within text
	where   string // the WHERE clause ("" when absent)
	upsert  bool   // the UPDATE is an INSERT's ON CONFLICT … DO UPDATE arm
	valOff  int    // offset of text within the literal's value
	lit     *ast.BasicLit
	raw     bool // backtick literal: value bytes are source bytes
}

// at converts an offset within the statement text back to a token.Pos.
// Raw literals map value bytes one-to-one onto source bytes (the +1
// steps over the opening backtick). Interpreted literals are
// single-line, so a position anywhere inside the statement lands on the
// statement's line regardless of escape-shortened offsets.
func (s sqlStmt) at(off int) token.Pos {
	if !s.raw {
		return s.lit.Pos() + 1 + token.Pos(min(off, len(s.text)))
	}
	return s.lit.Pos() + 1 + token.Pos(s.valOff+min(off, len(s.text)))
}

var (
	// reUpsertArm matches the text ending just before an upsert's
	// UPDATE: INSERT … ON CONFLICT (…) DO UPDATE.
	reUpsertArm = regexp.MustCompile(`(?i)\bDO\s+$`)
	reSQLVerb   = regexp.MustCompile(`(?i)\b(UPDATE|DELETE)\b`)
	reSQLSet    = regexp.MustCompile(`(?i)\bSET\b`)
	reSQLWhre   = regexp.MustCompile(`(?i)\bWHERE\b`)
)

// sqlStatements walks the file's string literals and returns every
// UPDATE/DELETE statement they contain. Reading literals through the AST
// means commented-out SQL and prose in comments cannot produce a
// statement. Statements are split on ';' inside a literal; none of the
// repo's queue SQL carries a semicolon elsewhere (a CASE WHEN never
// does), and a split inside a quoted value would merely yield two
// chunks of which at most one matches a verb.
func sqlStatements(file *ast.File) []sqlStmt {
	var out []sqlStmt
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if !reSQLVerb.MatchString(value) {
			return true
		}
		raw := strings.HasPrefix(lit.Value, "`")
		valOff := 0
		for _, chunk := range strings.Split(value, ";") {
			m := reSQLVerb.FindStringSubmatchIndex(chunk)
			if m == nil {
				valOff += len(chunk) + 1
				continue
			}
			stmt := sqlStmt{
				verb:    strings.ToLower(chunk[m[2]:m[3]]),
				verbOff: m[2],
				text:    chunk,
				valOff:  valOff,
				lit:     lit,
				raw:     raw,
			}
			stmt.upsert = stmt.verb == "update" && reUpsertArm.MatchString(chunk[:m[2]])
			stmt.set, stmt.setOff = clause(chunk, reSQLSet, reSQLWhre)
			stmt.where, _ = clause(chunk, reSQLWhre, nil)
			out = append(out, stmt)
			valOff += len(chunk) + 1
		}
		return true
	})
	return out
}

// clause slices the text from the start marker to the end marker (or to
// the end of the statement when end is nil), returning the clause and
// its offset within text.
func clause(text string, start, end *regexp.Regexp) (string, int) {
	loc := start.FindStringIndex(text)
	if loc == nil {
		return "", 0
	}
	if end != nil {
		if e := end.FindStringIndex(text[loc[1]:]); e != nil {
			return text[loc[0] : loc[1]+e[0]], loc[0]
		}
	}
	return text[loc[0]:], loc[0]
}
