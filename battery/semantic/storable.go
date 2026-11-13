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

// InvalidDocumentTextError identifies a document field containing invalid
// UTF-8 or a NUL byte. Add and the owned keyword adapter refuse it before
// invoking a storage backend, including an in-memory backend.
type InvalidDocumentTextError struct {
	Field string
}

func (err *InvalidDocumentTextError) Error() string {
	return fmt.Sprintf("semantic: document %s must be valid UTF-8 without NUL bytes", err.Field)
}

// MetadataNumberRangeError identifies document metadata holding a JSON
// number Postgres jsonb cannot store: more than 131072 digits before the
// decimal point or 16383 after it. Add refuses it on every store.
type MetadataNumberRangeError struct{}

func (*MetadataNumberRangeError) Error() string {
	return "semantic: document metadata holds a number outside the range Postgres jsonb stores (131072 digits before the decimal point, 16383 after)"
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
	// kind is also bound raw to PgVectorStore's text column; JSON's
	// UTF-8 normalization cannot validate that separate representation.
	if kind, ok := document.Metadata["kind"].(string); ok && !textsafe.Storable(kind) {
		return &InvalidDocumentTextError{Field: "metadata"}
	}
	if len(document.Metadata) == 0 {
		return nil
	}
	// PgVectorStore marshals metadata the same way; refusing here keeps
	// every store accepting the same documents.
	encoded, err := json.Marshal(document.Metadata)
	if err != nil {
		return fmt.Errorf("semantic: document metadata cannot be encoded as JSON: %w", err)
	}
	return storableMetadataJSON(encoded)
}

// storableMetadataJSON checks exactly the encoded value the JSONB column
// will hold, including custom encoders and fields omitted by JSON tags.
// UseNumber preserves JSON numbers outside float64's range during the NUL
// token walk. Checking every string token also retains duplicate object
// members that decoding into a map would discard. The decoder turns a
// lone surrogate escape into U+FFFD, while Postgres's jsonb input refuses
// it, so escapes are checked on the encoded bytes first. A decoding
// failure refuses the metadata. Number tokens keep their literal text, so
// a number past jsonb's numeric range is refused here, not at the store.
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

// Postgres numeric limits as jsonb applies them, measured on Postgres 16.
const (
	jsonbMaxIntegerDigits  = 131072
	jsonbMaxFractionDigits = 16383
	jsonbMaxExponent       = 1<<30 - 1 // INT_MAX/2 in numeric's parser
)

// jsonbNumberFits reports whether a JSON number literal (already valid
// JSON) fits Postgres numeric: the digits after the decimal point count
// even for zero (0.0000e-20000 is refused), and the digits before it are
// counted from the first nonzero digit.
func jsonbNumberFits(literal string) bool {
	mantissa, exponentText, _ := strings.Cut(strings.ToLower(strings.TrimPrefix(literal, "-")), "e")
	exponent := int64(0)
	if exponentText != "" {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return false // beyond any digit count jsonb stores either way
		}
		exponent = parsed
	}
	if exponent > jsonbMaxExponent {
		return false // refused before the digits are counted, zero included
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	if int64(len(fraction))-exponent > jsonbMaxFractionDigits {
		return false
	}
	digits := integer + fraction
	first := strings.IndexFunc(digits, func(r rune) bool { return r != '0' })
	if first < 0 {
		return true // zero has no integer digits to overflow
	}
	// Power of ten of the leading nonzero digit.
	weight := int64(len(integer)-1-first) + exponent
	return weight < jsonbMaxIntegerDigits
}

// loneSurrogateEscape reports a \uD800-\uDFFF escape in encoded JSON that
// is not a high surrogate followed directly by a low one. Outside strings
// a backslash is invalid JSON, which the token walk refuses.
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
			i++ // skip the escaped character, a backslash included
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
