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
	return Members(doc, func(_, s string) string { return redact(s) })
}

// Members is Strings with the member name in view: redact receives, for
// a string that is an object member's value, the member's name as key,
// and "" for every other string (object keys and array elements). A
// redactor matching NAME=value text cannot see {"NAME": "value"}, where
// the name and the value are two strings; key lets it judge the value
// by the name it sits under.
func Members(doc []byte, redact func(key, s string) string) (out []byte, ok bool) {
	if !json.Valid(doc) {
		return nil, false
	}
	out = make([]byte, 0, len(doc))
	// valueKey names the member whose value comes next; expectValue is
	// set from a member name until the value's first byte.
	var valueKey string
	expectValue := false
	for i := 0; i < len(doc); {
		if doc[i] != '"' {
			if expectValue && !isJSONSpace(doc[i]) && doc[i] != ':' {
				// The value is an object, array, number or literal.
				expectValue, valueKey = false, ""
			}
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
		key := ""
		if expectValue {
			key = valueKey
			expectValue, valueKey = false, ""
		} else if isMemberName(doc, i) {
			expectValue, valueKey = true, s
		}
		r := redact(key, s)
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

// isMemberName reports whether the string literal that ended just before
// doc[i] is an object member's name: the next non-space byte is a colon.
func isMemberName(doc []byte, i int) bool {
	for ; i < len(doc); i++ {
		if !isJSONSpace(doc[i]) {
			return doc[i] == ':'
		}
	}
	return false
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
