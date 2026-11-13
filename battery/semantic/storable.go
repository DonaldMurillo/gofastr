package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	if !storableMetadataJSON(document.Metadata) {
		return &InvalidDocumentTextError{Field: "metadata"}
	}
	return nil
}

// storableMetadataJSON checks exactly the encoded value the JSONB column
// will hold, including custom encoders and fields omitted by JSON tags.
// UseNumber preserves JSON numbers outside float64's range during the NUL
// token walk. Checking every string token also retains duplicate object
// members that decoding into a map would discard. Encoding and decoding
// failures refuse metadata before storage.
func storableMetadataJSON(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return true
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return false
	}
	if !utf8.Valid(encoded) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		if value, ok := token.(string); ok && !textsafe.Storable(value) {
			return false
		}
	}
}
