// Package jsonredact runs a text redactor over the string values of a
// JSON document.
//
// A regex run on serialized JSON sees escapes, not text: a match can
// swallow a string's closing quote or start inside a < escape, and
// the payload it leaves behind no longer parses. Redacting each decoded
// string and encoding it again keeps the document valid whatever the
// pattern matches. Everything outside the changed strings is kept byte
// for byte, so field order and numbers are untouched.
package jsonredact

import "encoding/json"

// Strings applies redact to every string literal in doc (object keys
// included) and returns the rewritten document. ok is false when doc is
// not valid JSON; the caller decides what to do with such a payload.
func Strings(doc []byte, redact func(string) string) (out []byte, ok bool) {
	if !json.Valid(doc) {
		return nil, false
	}
	out = make([]byte, 0, len(doc))
	for i := 0; i < len(doc); {
		if doc[i] != '"' {
			out = append(out, doc[i])
			i++
			continue
		}
		// doc is valid JSON, so a quote outside a string opens a literal
		// and the literal ends at the next unescaped quote.
		j := i + 1
		for doc[j] != '"' {
			if doc[j] == '\\' {
				j++
			}
			j++
		}
		lit := doc[i : j+1]
		i = j + 1
		var s string
		if err := json.Unmarshal(lit, &s); err != nil {
			return nil, false
		}
		r := redact(s)
		if r == s {
			out = append(out, lit...)
			continue
		}
		enc, err := json.Marshal(r)
		if err != nil {
			return nil, false
		}
		out = append(out, enc...)
	}
	return out, true
}
