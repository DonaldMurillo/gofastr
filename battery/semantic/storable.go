package semantic

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
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
	if !storableMetadata(reflect.ValueOf(document.Metadata), make(map[reflect.Value]bool)) || !storableMetadataJSON(document.Metadata) {
		return &InvalidDocumentTextError{Field: "metadata"}
	}
	return nil
}

// storableMetadataJSON checks metadata as the JSONB column will hold it. A
// value with its own encoding (json.RawMessage, a MarshalJSON method) is
// bytes to the reflection walk, yet an escaped NUL in its output is a NUL
// to Postgres (22P05), and Marshal copies its invalid UTF-8 through as is.
// A marshal error is left to the store, which reports it as it always has.
func storableMetadataJSON(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return true
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return true
	}
	if !utf8.Valid(encoded) {
		return false
	}
	var decoded any
	if err := jsonexact.Unmarshal(encoded, &decoded); err != nil {
		return true
	}
	return noNULInJSON(decoded)
}

// noNULInJSON walks a decoded JSON value for a NUL in any string or key.
// Decoding already turned every escape into its byte, so a NUL found here
// is one the stored JSONB would carry.
func noNULInJSON(value any) bool {
	switch value := value.(type) {
	case string:
		return strings.IndexByte(value, 0) < 0
	case map[string]any:
		for key, element := range value {
			if strings.IndexByte(key, 0) >= 0 || !noNULInJSON(element) {
				return false
			}
		}
	case []any:
		for _, element := range value {
			if !noNULInJSON(element) {
				return false
			}
		}
	}
	return true
}

func storableMetadata(value reflect.Value, seen map[reflect.Value]bool) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.String:
		return textsafe.Storable(value.String())
	case reflect.Interface:
		return storableMetadata(value.Elem(), seen)
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() || seen[value] {
			return true
		}
		seen[value] = true
	}
	switch value.Kind() {
	case reflect.Pointer:
		return storableMetadata(value.Elem(), seen)
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if !storableMetadata(iterator.Key(), seen) || !storableMetadata(iterator.Value(), seen) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if !storableMetadata(value.Index(index), seen) {
				return false
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).IsExported() && !storableMetadata(value.Field(index), seen) {
				return false
			}
		}
	}
	return true
}
