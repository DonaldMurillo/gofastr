// Package dsnredact strips credentials from database DSNs so the
// remainder can be logged or committed (host/db name are configuration,
// not secrets). It is the one canonical redactor, lifted verbatim from
// cmd/gofastr/blueprint.go::redactDSN (the rules there were proven
// against the round-5 probes; the two test-infra twins that drifted from
// them are its callers now).
//
// Rules:
//
//   - A DSN that carries no detectable secret is returned unchanged:
//     SQLite file DSNs and passwordless URLs round-trip verbatim.
//   - URL form: url.Parse first; a userinfo password is dropped by
//     rebuilding the URL without it. A URL url.Parse rejects (e.g. a
//     password containing a bad % escape) is cut TEXTUALLY at the LAST
//     '@' before the path — the password may itself contain '@', so a
//     first-'@' cut leaves password material past the mask. Fails closed:
//     an unparsable URL that carries a userinfo section is treated as
//     secret-bearing.
//   - key=value form (libpq): the password pair is dropped whole. Values
//     may be single-quoted and contain spaces (password='a b'), so fields
//     split quote-aware; a bare strings.Fields would leak the quoted tail.
package dsnredact

import (
	"net/url"
	"strings"
)

// Redact returns dsn with its password removed for safe logging.
func Redact(dsn string) string {
	if !HasSecret(dsn) {
		return dsn
	}
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.User(u.User.Username())
			return u.String()
		}
	}
	if i := strings.Index(dsn, "://"); i >= 0 {
		// URL-form DSN that url.Parse rejected (e.g. a bad % escape in
		// the password): strip the userinfo textually. The password may
		// itself contain '@', so cut at the last '@' before the path.
		rest := dsn[i+3:]
		end := len(rest)
		for _, sep := range []byte{'/', '?', '#'} {
			if j := strings.IndexByte(rest, sep); j >= 0 && j < end {
				end = j
			}
		}
		if at := strings.LastIndex(rest[:end], "@"); at >= 0 {
			user := rest[:at]
			if colon := strings.IndexByte(user, ':'); colon >= 0 {
				user = user[:colon]
			}
			return dsn[:i+3] + user + "@" + rest[at+1:]
		}
		return dsn
	}
	// key=value form: drop the password pair. Values may be libpq
	// single-quoted and contain spaces (password='a b'), so split
	// quote-aware; a bare strings.Fields would leak the quoted tail.
	fields := splitFields(dsn)
	kept := fields[:0]
	for _, f := range fields {
		if strings.HasPrefix(f, "password=") {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

// HasSecret reports whether dsn carries a detectable credential: a
// url.Parse-able userinfo password, a textual userinfo section on a URL
// url.Parse rejects, or a `password=` pair. SQLite file DSNs return
// false: nothing to hide.
//
// Fails CLOSED on URL-form DSNs that url.Parse rejects: if there is a
// userinfo section we cannot prove holds no credential, treat it as
// secret-bearing rather than passing it through verbatim.
//
// Exported as the one credential-shape rule for DSNs (Redact routes
// through it). It replaces the former in-tree copies: cmd/gofastr's
// dsnHasSecret and kiln/freeze's DSNHasSecret, which were kept in
// lockstep only by parallel security tests.
func HasSecret(dsn string) bool {
	if dsn == "" {
		return false
	}
	if strings.Contains(dsn, "password=") {
		return true
	}
	if u, err := url.Parse(dsn); err == nil {
		if u.User != nil {
			if _, has := u.User.Password(); has {
				return true
			}
		}
	} else if i := strings.Index(dsn, "://"); i >= 0 && strings.Contains(dsn[i+3:], "@") {
		return true
	}
	return false
}

// splitFields splits a libpq key/value DSN on whitespace, keeping
// single-quoted values (with \' escapes) intact so a quoted password is
// dropped whole rather than leaking its tail.
func splitFields(dsn string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for i := range len(dsn) {
		c := dsn[i]
		switch {
		case escaped:
			escaped = false
			cur.WriteByte(c)
		case c == '\\' && inQuote:
			escaped = true
			cur.WriteByte(c)
		case c == '\'':
			inQuote = !inQuote
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}
