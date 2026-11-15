package crud

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func coerceHandler() *CrudHandler {
	return NewCrudHandler(entity.Define("things", entity.EntityConfig{
		Table: "things",
		Fields: []schema.Field{
			{Name: "count", Type: schema.Int},
			{Name: "done", Type: schema.Bool},
		},
	}.WithTimestamps(false)), nil)
}

// A write binds an Int column's decimal text as the int64 and a Bool
// column's accepted spelling as the boolean.
func TestCoerceBindsIntTextAndBoolSpelling(t *testing.T) {
	body := map[string]any{"count": "42", "done": "true"}
	if err := coerceHandler().coerceNumberColumnValues(body); err != nil {
		t.Fatal(err)
	}
	if body["count"] != int64(42) || body["done"] != true {
		t.Fatalf("coerced body %#v, want count int64(42) and done true", body)
	}
}

// An integral JSON number past ±2^53 that is not an integer literal (1e16)
// is refused on an Int column rather than stored as its float64 rounding.
func TestCoerceRefusesExponentIntPastExactRange(t *testing.T) {
	err := coerceHandler().coerceNumberColumnValues(map[string]any{"count": json.Number("1e16")})
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Fields()["count"]) == 0 || ve.Fields()["count"][0].Reason != reasonBeyondPrecision {
		t.Fatalf("count 1e16: %v, want the beyond-precision field error", err)
	}
}

// The pass before the Before hooks refuses nothing: a value it cannot read
// stays as sent and every other column is still converted.
func TestCoerceBeforeHooksConvertsPastAnUnreadableValue(t *testing.T) {
	body := map[string]any{"count": json.Number("1.5"), "done": "true"}
	coerceHandler().coerceBeforeHooks(schema.Schema{}, body, false)
	if body["count"] != json.Number("1.5") || body["done"] != true {
		t.Fatalf("body after the lenient pass %#v, want count as sent and done true", body)
	}
}

// Stored text holding more than one JSON value is not a document: it reads
// back as the text, not as its first value.
func TestStoredTextWithTrailingValueReadsAsText(t *testing.T) {
	const stored = `{"a":1} {"b":2}`
	if got := decodeJSONColumn(stored); got != stored {
		t.Fatalf("decodeJSONColumn(%q) = %#v, want the text", stored, got)
	}
}
