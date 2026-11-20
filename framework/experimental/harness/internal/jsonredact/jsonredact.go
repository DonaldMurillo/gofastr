// Package jsonredact runs a text redactor over the decoded string values of a JSON document.
package jsonredact

import "encoding/json"

// Strings applies redact to every string in doc, object keys included; ok is false when doc is not valid JSON.
func Strings(doc []byte, redact func(string) string) (out []byte, ok bool) {
	return Members(doc, func(_, s string) string { return redact(s) })
}

// Members is Strings that also passes the member name as key for a member's string value, and "" for every other string.
func Members(doc []byte, redact func(key, s string) string) (out []byte, ok bool) {
	if !json.Valid(doc) {
		return nil, false
	}
	out = make([]byte, 0, len(doc))
	var valueKey string
	expectValue := false
	for i := 0; i < len(doc); {
		if doc[i] != '"' {
			if expectValue && !isJSONSpace(doc[i]) && doc[i] != ':' {
				expectValue, valueKey = false, ""
			}
			out = append(out, doc[i])
			i++
			continue
		}
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
