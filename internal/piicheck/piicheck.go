// Package piicheck shares the PII lint policy for Go and app-spec declarations.
package piicheck

import (
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Scoped reports whether the declaration has ownership, tenancy, or an access gate.
func Scoped(ownerField string, multiTenant, accessGate bool) bool {
	return ownerField != "" || multiTenant || accessGate
}

// GatesAccess reports whether a declared permission restricts its operation.
func GatesAccess(permission string) bool {
	p := strings.TrimSpace(permission)
	return p != "" && p != entity.Anyone
}

var piiTokens = map[string]bool{
	"email": true, "phone": true, "mobile": true, "address": true,
	"street": true, "ssn": true, "password": true, "passwd": true,
	"token": true, "secret": true, "card": true, "iban": true,
	"dob": true, "birthday": true, "birthdate": true, "passport": true,
	"salary": true,
}

// Field reports whether a column name contains a whole PII token.
func Field(name string) bool {
	for _, tok := range splitIdentifier(name) {
		if piiTokens[tok] {
			return true
		}
	}
	return false
}

func splitIdentifier(s string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(runes[i-1]) ||
				(i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				flush()
			}
			cur.WriteRune(unicode.ToLower(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}
