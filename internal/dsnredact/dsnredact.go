// Package dsnredact strips credentials from database DSNs so the
// remainder can be logged or committed (host/db name are configuration,
// not secrets). It is the one canonical redactor, lifted verbatim from
// cmd/gofastr/internal/appgen/appspec.go::redactDSN (the rules there were proven
// against the round-5 probes; the two test-infra twins that drifted from
// them are its callers now).
//
// Rules:
//
//   - A DSN that carries no detectable secret is returned unchanged:
//     SQLite file DSNs and passwordless URLs round-trip verbatim.
//   - URL form: userinfo passwords and password/sslpassword query parameters
//     are removed. Unreadable query strings are dropped in full.
//     A URL url.Parse rejects (e.g. a
//     password containing a bad % escape) is cut TEXTUALLY at the LAST
//     '@' before removing query or fragment text. Raw delimiters inside
//     malformed passwords cannot safely delimit the authority; ambiguous
//     at signs across those delimiters discard the whole remainder. Fails closed:
//     an unparsable URL that carries a userinfo section is treated as
//     secret-bearing.
//   - key=value form (libpq): password and sslpassword assignments are dropped
//     whole, allowing whitespace around '=' and quoted or escaped values.
//     Splitting on whitespace alone would leak the password's tail.
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
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		if u.User != nil {
			u.User = url.User(u.User.Username())
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			u.RawQuery = ""
		} else if query.Has("password") || query.Has("sslpassword") {
			query.Del("password")
			query.Del("sslpassword")
			u.RawQuery = query.Encode()
		}
		return u.String()
	}
	if i := strings.Index(dsn, "://"); i >= 0 {
		rest := dsn[i+3:]
		if at := strings.LastIndexByte(rest, '@'); at >= 0 {
			if separator := strings.IndexAny(rest, "/?#"); separator >= 0 && strings.IndexByte(rest, '@') < separator && separator < at {
				return dsn[:i+3]
			}
			user := rest[:at]
			if colon := strings.IndexByte(user, ':'); colon >= 0 {
				user = user[:colon]
			}
			rest = user + "@" + rest[at+1:]
		}
		if query := strings.IndexAny(rest, "?#"); query >= 0 {
			rest = rest[:query]
		}
		return dsn[:i+3] + rest
	}
	// key=value form: drop the password pair. Values may be libpq
	// single-quoted and contain spaces (password='a b'), so split
	// quote-aware; a bare strings.Fields would leak the quoted tail.
	fields := splitFields(dsn)
	kept := fields[:0]
	for _, f := range fields {
		key, _, _ := strings.Cut(f, "=")
		key = strings.TrimSpace(key)
		if key == "password" || key == "sslpassword" {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

// HasSecret reports whether dsn carries a detectable credential: a
// url.Parse-able userinfo password, a textual userinfo section on a URL
// url.Parse rejects, a password/sslpassword URL query parameter, or a libpq
// password/sslpassword assignment with optional whitespace around '='.
// SQLite file DSNs without credentials return
// false: nothing to hide.
//
// Fails CLOSED on URL-form DSNs that url.Parse rejects: if there is a
// userinfo section or query we cannot prove holds no credential, treat it as
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
	for _, field := range splitFields(dsn) {
		key, _, found := strings.Cut(field, "=")
		key = strings.TrimSpace(key)
		if found && (key == "password" || key == "sslpassword") {
			return true
		}
	}
	if u, err := url.Parse(dsn); err == nil {
		if u.Scheme != "" {
			query, err := url.ParseQuery(u.RawQuery)
			if err != nil || query.Has("password") || query.Has("sslpassword") {
				return true
			}
		}
		if u.User != nil {
			if _, has := u.User.Password(); has {
				return true
			}
		}
	} else if i := strings.Index(dsn, "://"); i >= 0 && strings.ContainsAny(dsn[i+3:], "@?") {
		return true
	}
	return false
}

// splitFields reads whole libpq assignments, including whitespace around '=',
// single-quoted values and backslash escapes inside or outside quotes.
func splitFields(dsn string) []string {
	var fields []string
	for offset := 0; offset < len(dsn); {
		for offset < len(dsn) && dsnSpace(dsn[offset]) {
			offset++
		}
		start := offset
		for offset < len(dsn) && !dsnSpace(dsn[offset]) && dsn[offset] != '=' {
			offset++
		}
		keyEnd := offset
		for offset < len(dsn) && dsnSpace(dsn[offset]) {
			offset++
		}
		if offset >= len(dsn) || dsn[offset] != '=' {
			if keyEnd > start {
				fields = append(fields, dsn[start:keyEnd])
			}
			continue
		}
		offset++
		for offset < len(dsn) && dsnSpace(dsn[offset]) {
			offset++
		}
		inQuote := offset < len(dsn) && dsn[offset] == '\''
		if inQuote {
			offset++
		}
		for offset < len(dsn) {
			char := dsn[offset]
			if char == '\\' && offset+1 < len(dsn) {
				offset += 2
				continue
			}
			if inQuote && char == '\'' {
				offset++
				break
			}
			if dsnSpace(char) && !inQuote {
				break
			}
			offset++
		}
		fields = append(fields, dsn[start:offset])
	}
	return fields
}

func dsnSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\v' || char == '\f'
}
