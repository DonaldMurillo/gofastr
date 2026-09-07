package dsnredact

import (
	"strings"
	"testing"
)

// TestRedact is the canonical table for the one DSN redactor, lifted from
// cmd/gofastr's proven rules plus every probe shape the round-5 red tests
// pinned on the drifted twins (testdb cut at the FIRST '@' so an '@'
// inside the password leaked its tail; pgtest returned key=value DSNs
// verbatim).
func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// want, when non-empty, is the exact expected output.
		want string
		// leaks, when non-empty, are substrings that must NOT appear.
		leaks []string
		// keeps, when non-empty, are substrings that MUST appear.
		keeps []string
	}{
		{
			name: "url password masked, host kept",
			in:   "postgres://admin:hunter2@db.example:5432/app",
			want: "postgres://admin@db.example:5432/app",
		},
		{
			name:  "at inside password fully masked (last-@ cut)",
			in:    "postgres://admin:p@ss@db.example:5432/app",
			leaks: []string{"p@ss", "ss@", "hunter"},
			keeps: []string{"db.example:5432/app"},
		},
		{
			name:  "unparsable url userinfo cut textually",
			in:    "postgres://admin:bad%zz@db.example:5432/app",
			leaks: []string{"bad%zz"},
			keeps: []string{"db.example"},
		},
		{
			name: "passwordless url round-trips verbatim",
			in:   "postgres://admin@db.example:5432/app",
			want: "postgres://admin@db.example:5432/app",
		},
		{
			name: "key=value password pair dropped",
			in:   "host=localhost user=admin password=hunter2 dbname=app",
			want: "host=localhost user=admin dbname=app",
		},
		{
			name:  "single-quoted password with space dropped whole",
			in:    "host=localhost password='quoted secret' user=admin", // not-a-secret: redaction fixture, the password is the thing being scrubbed
			leaks: []string{"quoted secret"},
			keeps: []string{"host=localhost", "user=admin"},
		},
		{
			name: "passwordless key=value round-trips verbatim",
			in:   "host=localhost user=admin dbname=app sslmode=disable",
			want: "host=localhost user=admin dbname=app sslmode=disable",
		},
		{
			name: "sqlite file dsn untouched",
			in:   "file:/var/lib/app/data.db?_fk=1",
			want: "file:/var/lib/app/data.db?_fk=1",
		},
		{
			name: "url with query after path cuts userinfo at last @ before path",
			in:   "postgres://u:p@h.io:5432/db?sslmode=disable",
			want: "postgres://u@h.io:5432/db?sslmode=disable",
		},
		{
			name: "empty dsn untouched",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, leak := range tc.leaks {
				if strings.Contains(got, leak) {
					t.Errorf("SECURITY: [dsnredact] Redact(%q) = %q leaks %q", tc.in, got, leak)
				}
			}
			for _, keep := range tc.keeps {
				if !strings.Contains(got, keep) {
					t.Errorf("SECURITY: [dsnredact] Redact(%q) = %q lost %q (the redactor strips credentials, not configuration)", tc.in, got, keep)
				}
			}
		})
	}
}

// TestRedactIdempotent: redacting an already-redacted DSN is a no-op (a
// logged-then-redacted-again value must not keep shrinking).
func TestRedactIdempotent(t *testing.T) {
	once := Redact("postgres://admin:hunter2@db.example:5432/app")
	if again := Redact(once); again != once {
		t.Errorf("Redact not idempotent: %q → %q", once, again)
	}
	kv := Redact("host=localhost password=hunter2 user=admin")
	if again := Redact(kv); again != kv {
		t.Errorf("Redact not idempotent on key=value: %q → %q", kv, again)
	}
}
