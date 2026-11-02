// Package spellings holds the identity rewrites the 2026-11-02 guard
// bash found keytrim blind to: the same trim spelled strings.Trim /
// TrimLeft / TrimRight / TrimFunc or bytes.TrimSpace, and a request read
// that reaches TrimSpace through a local.
package spellings

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"
)

func trim(id string) string {
	return strings.Trim(id, " ") // want `keytrim: strings\.Trim rewrites the identity value id`
}

func trimRight(tenantKey string) string {
	return strings.TrimRight(tenantKey, " \t\n") // want `keytrim: strings\.TrimRight rewrites the identity value tenantKey`
}

func trimFunc(userID string) string {
	return strings.TrimFunc(userID, unicode.IsSpace) // want `keytrim: strings\.TrimFunc rewrites the identity value userID`
}

func trimBytes(recordID []byte) []byte {
	return bytes.TrimSpace(recordID) // want `keytrim: bytes\.TrimSpace rewrites the identity value recordID`
}

func throughLocal(vals url.Values) string {
	raw := vals.Get("user_id")
	return strings.TrimSpace(raw) // want `keytrim: strings\.TrimSpace rewrites the identity value raw`
}

// Silent: a non-identity operand, a presence check, file contents.
func quiet(name string, b []byte, id string) (string, bool) {
	return strings.Trim(name, " "), strings.Trim(id, " ") == ""
}

func contents(b []byte) string {
	return strings.TrimSpace(string(b))
}
