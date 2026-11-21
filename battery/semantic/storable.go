package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// InvalidDocumentTextError identifies a document field holding invalid UTF-8
// or a NUL byte.
type InvalidDocumentTextError struct {
	Field string
}

func (err *InvalidDocumentTextError) Error() string {
	return fmt.Sprintf("semantic: document %s must be valid UTF-8 without NUL bytes", err.Field)
}

type MetadataNumberRangeError struct{}

func (*MetadataNumberRangeError) Error() string {
	return "semantic: document metadata holds a number outside the range Postgres jsonb stores (131072 digits before the decimal point, 16383 after, exponent 1073741823)"
}

type InvalidQueryError struct {
	Reason string
}

func (err *InvalidQueryError) Error() string { return "semantic: " + err.Reason }

func validateQueryText(q Query) error {
	for _, field := range []struct{ name, value string }{
		{"text", q.Text},
		{"filter.source", q.Filter.Source},
		{"filter.kind", q.Filter.Kind},
	} {
		if !textsafe.Storable(field.value) {
			return &InvalidQueryError{Reason: "query " + field.name + " must be valid UTF-8 without NUL bytes"}
		}
	}
	if len(q.Filter.MetaMatch) == 0 {
		return nil
	}
	encoded, err := json.Marshal(q.Filter.MetaMatch)
	if err == nil {
		err = storableMetadataJSON(encoded)
	}
	if err != nil {
		return &InvalidQueryError{Reason: "query filter.meta_match holds a value no document metadata can: " + err.Error()}
	}
	return nil
}

func validateDocumentText(document Document) error {
	for _, field := range []struct{ name, value string }{
		{"id", document.ID},
		{"source", document.Source},
		{"text", document.Text},
		{"owner", document.Owner},
	} {
		if !textsafe.Storable(field.value) {
			return &InvalidDocumentTextError{Field: field.name}
		}
	}
	if raw, present := document.Metadata["kind"]; present {
		if kind, isString := raw.(string); !isString || !textsafe.Storable(kind) {
			return &InvalidDocumentTextError{Field: "metadata"}
		}
	}
	if len(document.Metadata) == 0 {
		return nil
	}
	encoded, err := json.Marshal(document.Metadata)
	if err != nil {
		return fmt.Errorf("semantic: document metadata cannot be encoded as JSON: %w", err)
	}
	return storableMetadataJSON(encoded)
}

func storableMetadataJSON(encoded []byte) error {
	invalid := &InvalidDocumentTextError{Field: "metadata"}
	if !utf8.Valid(encoded) || loneSurrogateEscape(encoded) {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid
		}
		switch value := token.(type) {
		case string:
			if !textsafe.Storable(value) {
				return invalid
			}
		case json.Number:
			if !jsonbNumberFits(string(value)) {
				return &MetadataNumberRangeError{}
			}
		}
	}
}

const (
	jsonbMaxIntegerDigits  = 131072
	jsonbMaxFractionDigits = 16383
	jsonbMaxExponent       = 1<<30 - 1
)

func jsonbNumberFits(literal string) bool {
	mantissa, exponentText, _ := strings.Cut(strings.ToLower(strings.TrimPrefix(literal, "-")), "e")
	exponent := int64(0)
	if exponentText != "" {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return false
		}
		exponent = parsed
	}
	if exponent > jsonbMaxExponent {
		return false
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	if int64(len(fraction))-exponent > jsonbMaxFractionDigits {
		return false
	}
	digits := integer + fraction
	first := strings.IndexFunc(digits, func(r rune) bool { return r != '0' })
	if first < 0 {
		return true
	}
	weight := int64(len(integer)-1-first) + exponent
	return weight < jsonbMaxIntegerDigits
}

func loneSurrogateEscape(encoded []byte) bool {
	pendingHigh := false
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '\\' || i+1 >= len(encoded) {
			if pendingHigh {
				return true
			}
			continue
		}
		if encoded[i+1] != 'u' {
			if pendingHigh {
				return true
			}
			i++
			continue
		}
		r, ok := hexRune(encoded[i+2:])
		if !ok {
			return true
		}
		switch {
		case r >= 0xD800 && r <= 0xDBFF:
			if pendingHigh {
				return true
			}
			pendingHigh = true
		case r >= 0xDC00 && r <= 0xDFFF:
			if !pendingHigh {
				return true
			}
			pendingHigh = false
		default:
			if pendingHigh {
				return true
			}
		}
		i += 5
	}
	return pendingHigh
}

func hexRune(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}
