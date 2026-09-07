//go:build red

package pgtest

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a DSN handed to a helper documented "for safe logging" (RedactDSN's own doc
// comment) never carries its password into the logged value. The canonical redactor
// cmd/gofastr/blueprint.go::redactDSN pins the property for BOTH shapes — URL userinfo
// masked, and key=value pairs dropped quote-aware (splitDSNFields handles
// password='a b'); this internal twin drifted to a url.Parse-with-userinfo arm only.
// Surfaces: internal/pgtest/pgtest.go::RedactDSN :225-234 — the key=value arm falls
// through to `return dsn` verbatim at :233; the redacted value is logged by DB :129,
// FreshDatabaseDSN :176, and UnusedDSN :209.
// Finding: RedactDSN("host=localhost user=admin password=hunter2 dbname=app") returns
// the input unchanged, password included (verified 2026-09-06).
// Fix direction: mirror blueprint.go::redactDSN's key=value arm (quote-aware
// password-pair drop), or delegate to it.

import (
	"strings"
	"testing"
)

func TestRedactRedKeyValuePasswordStripped(t *testing.T) {
	arms := []struct {
		name string
		dsn  string
		leak string
	}{
		{
			name: "bare key=value password",
			dsn:  "host=localhost user=admin password=hunter2 dbname=app",
			leak: "hunter2",
		},
		{
			name: "single-quoted password with space",
			dsn:  "host=localhost password='quoted secret' user=admin", // not-a-secret: red-test fixture proving RedactDSN strips quoted libpq passwords
			leak: "quoted secret",                                      // not-a-secret: substring the assertion must NOT find in the redacted output
		},
	}
	for _, arm := range arms {
		out := RedactDSN(arm.dsn)
		if strings.Contains(out, arm.leak) {
			t.Errorf("SECURITY: [pgtest-redact-keyvalue] RedactDSN(%q) = %q leaks the password verbatim: a DSN logged through this helper (DB/FreshDatabaseDSN/UnusedDSN) writes the secret into test output", arm.dsn, out)
		}
	}

	// Control: the URL arm still masks — this is the half that works today
	// and must keep working after the fix.
	if out := RedactDSN("postgres://admin:hunter2@db.example:5432/app"); strings.Contains(out, "hunter2") {
		t.Errorf("control broken: URL-form redaction regressed: %q", out)
	}

	// Control: a passwordless key=value DSN round-trips untouched.
	plain := "host=localhost user=admin dbname=app sslmode=disable"
	if out := RedactDSN(plain); out != plain {
		t.Errorf("control broken: passwordless key=value DSN should round-trip, got %q", out)
	}
}
