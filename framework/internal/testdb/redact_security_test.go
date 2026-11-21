package testdb

// Testdb and appgen both delegate to dsnredact.Redact. The old testdb redactor
// split at the first '@', leaking password tails into the PG test log.

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

	// CONTRACT-QUESTION sub-arm: key=value form. appspec.go::redactDSN drops the
	// password pair quote-aware; if the decided contract for THIS helper is instead
	// "key=value DSNs are developer input, out of scope" (cf. the sqlite/stdlib
	// compat decision), delete this arm — but then the doc comment must not claim
	// general DSN redaction.
	if out := RedactDSN("host=localhost password=hunter2 user=admin"); strings.Contains(out, "hunter2") {
		t.Errorf("SECURITY: [testdb-redact-atpassword] RedactDSN key=value arm returns the password verbatim: %q (CONTRACT-QUESTION — see file header)", out)
	}
}
