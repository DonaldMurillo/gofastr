package lib

import (
	"net/http"
	"strings"
)

// BearerToken parses the scheme: a cross-package parser.
func BearerToken(r *http.Request) (string, bool) { // want BearerToken:"parsesAuth"
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return "", false
	}
	return tok, true
}
