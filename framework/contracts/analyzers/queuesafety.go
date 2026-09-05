package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
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
// host value (attempts = $1 / ? / %s) instead of the relative
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
//   - the relative spellings (attempts = attempts + 1, and the CASE
//     decrement in battery/queue release): the column computes itself
//     in SQL, and no shared snapshot can under-count;
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
		for _, m := range reAbsCounter.FindAllStringIndex(stmt.set, -1) {
			d := diag(p, contracts.RuleAbsoluteAttempts, rel,
				stmt.at(stmt.setOff+m[0]),
				fmt.Sprintf("UPDATE SET writes the retry counter from a host value (%s), not the relative form: overlapping claimants each write back their own snapshot and under-count — increment in SQL (attempts = attempts + 1, the battery/queue claim spelling) or move the count out of the settle write",
					strings.TrimSpace(stmt.set[m[0]:m[1]])))
			out = append(out, d)
		}
	}
	return out
}

// reAbsCounter matches a counter column assigned a placeholder ($n, the
// sqlite ?) or a Sprintf verb. \d+ consumes the whole placeholder
// number, so $12 needs no lookahead of its own.
var reAbsCounter = regexp.MustCompile(`(?i)\b(?:attempts|retries|tries|failures)\s*=\s*(?:\$\d+|\?|%[sdv])`)

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
//   - carries none of the four postures that prove it cannot touch
//     someone's live claim:
//     a token predicate in the WHERE (the Ack/Nack fence),
//     the minting SET itself (SET claim_token = $n creates the
//     ownership the WHERE would otherwise have to match),
//     a staleness bound on the claim timestamp (claimed_at <= $n: a
//     re-claim refreshes claimed_at, so the bound can never match a
//     live re-claimant — the dead-letter sweep's fence),
//     a terminal status guard in the WHERE (status='failed'/'dead'/…:
//     a live claim holds status='claimed', so the predicate selects
//     rows nobody owns — Replay's fence).
//
// Deliberately silent on:
//   - files with no token-fenced sibling: the rule is about a table
//     whose completion paths established the fence and then missed one
//     writer; an unfenced table is a different (lease-shaped) design,
//     see framework/outbox;
//   - SELECTs, DDL, and INSERTs: they cannot retire or re-queue someone
//     else's claim;
//   - writes with no claim vocabulary (a status flip on a plain row): a
//     fence on the table does not make every column a claim column;
//   - _test.go and generated files (AppFiles already excludes both).
func ruleUnfencedClaim(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	stmts := sqlStatements(file)
	armed := false
	for _, stmt := range stmts {
		if reTokenFence.MatchString(stmt.where) {
			armed = true
			break
		}
	}
	if !armed {
		return nil
	}
	var out []contracts.Diagnostic
	for _, stmt := range stmts {
		switch {
		case reTokenFence.MatchString(stmt.where),
			reTokenCol.MatchString(stmt.set), // the claim mints its token
			reStaleClaimAt.MatchString(stmt.where),
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

var (
	reTokenCol       = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\b`)
	reTokenFence     = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s*(?:=|IS\b)`)
	reStaleClaimAt   = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|lease_until|lease_expires_at)\s*(?:<=|<)`)
	reTerminalStatus = regexp.MustCompile(`(?i)\bstatus\b[^;]*'(?:failed|dead|abandoned|dispatched|completed|done|succeeded)'`)
	reClaimedStatus  = regexp.MustCompile(`(?i)\bstatus\b[^;]*'claimed'`)
	reClaimVocab     = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|claim_token|lease_token|owner_token|lease_until|lease_expires_at|max_attempts|attempts)\b`)
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
	reSQLVerb = regexp.MustCompile(`(?i)\b(UPDATE|DELETE)\b`)
	reSQLSet  = regexp.MustCompile(`(?i)\bSET\b`)
	reSQLWhre = regexp.MustCompile(`(?i)\bWHERE\b`)
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
