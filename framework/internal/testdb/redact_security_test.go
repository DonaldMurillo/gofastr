package testdb

// Pins (2026-09-06 adversarial pass, round 5):
// password redaction is total, not prefix-deep. In a URL the userinfo ends at
// the LAST '@' before the path (url.Parse semantics; cmd/gofastr/blueprint.go::redactDSN
// :3806-3824 pins the textual last-@ cut for exactly this reason). This twin cuts at the
// FIRST '@', so an '@' inside the password leaves its tail past the mask.
// Surfaces: framework/internal/testdb/testdb.go::RedactDSN :350-361 — strings.Index (first
// '@') at :352; the half-masked value is logged via t.Logf :109 on the first PG-backed
// test of every framework run.
// Finding: RedactDSN("postgres://admin:p@ss@db.example:5432/app") =
// "postgres://admin:****@ss@db.example:5432/app" — the "ss" tail of the password
// survives the mask (verified 2026-09-06).
// Fix direction: cut the userinfo at the last '@' before the path, or url.Parse first
// and only fall back to a textual last-@ cut (blueprint.go shape).

import (
	"strings"
	"testing"
)

func TestRedactRedAtInPasswordTotallyMasked(t *testing.T) {
	out := RedactDSN("postgres://admin:p@ss@db.example:5432/app")
	if strings.Contains(out, "ss@") || strings.Contains(out, "p@ss") {
		t.Errorf("SECURITY: [testdb-redact-atpassword] RedactDSN(\"postgres://admin:p@ss@db.example:5432/app\") = %q leaves password material past the mask: the cut must be at the last '@' before the path, not the first; this value is t.Logf'd on the first PG-backed test of every run", out)
	}

	// Control: a password without '@' is fully removed and the host
	// survives. (The pre-fix twin masked it as ":****@"; the canonical
	// dsnredact.Redact drops the password pair entirely, which is
	// strictly safer, so the control pins intent, not the old spelling.)
	if out := RedactDSN("postgres://admin:hunter2@db.example:5432/app"); !strings.Contains(out, "@db.example") || strings.Contains(out, "hunter2") || strings.Contains(out, ":****") {
		t.Errorf("control broken: plain-password URL redaction regressed: %q", out)
	}

	// CONTRACT-QUESTION sub-arm: key=value form. blueprint.go::redactDSN drops the
	// password pair quote-aware; if the decided contract for THIS helper is instead
	// "key=value DSNs are developer input, out of scope" (cf. the sqlite/stdlib
	// compat decision), delete this arm — but then the doc comment must not claim
	// general DSN redaction.
	if out := RedactDSN("host=localhost password=hunter2 user=admin"); strings.Contains(out, "hunter2") {
		t.Errorf("SECURITY: [testdb-redact-atpassword] RedactDSN key=value arm returns the password verbatim: %q (CONTRACT-QUESTION — see file header)", out)
	}
}
