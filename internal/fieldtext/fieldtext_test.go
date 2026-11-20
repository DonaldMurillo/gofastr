package fieldtext

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The text a form control of each field type holds for each shape a
// value arrives in, and what the date and datetime-local inputs then take.
func TestForm(t *testing.T) {
	plus2 := time.FixedZone("plus2", 2*3600)
	day := time.Date(2026, 1, 2, 9, 30, 0, 0, plus2)
	for _, tc := range []struct {
		name  string
		typ   schema.FieldType
		in    any
		text  string
		input string
		step  float64
	}{
		{name: "string", typ: schema.String, in: "0x1F", text: "0x1F"},
		{name: "int", typ: schema.Int, in: 7, text: "7"},
		{name: "float", typ: schema.Float, in: 12.5, text: "12.5"},
		{name: "bool true", typ: schema.Bool, in: true, text: "true"},
		{name: "bool spelled yes", typ: schema.Bool, in: "yes", text: "true"},
		{name: "bool spelled off", typ: schema.Bool, in: "off", text: "false"},
		{name: "date time", typ: schema.Date, in: day, text: "2026-01-02", input: "2026-01-02"},
		{name: "date pointer", typ: schema.Date, in: &day, text: "2026-01-02", input: "2026-01-02"},
		{name: "date string with time", typ: schema.Date, in: "2026-01-02T00:00:00Z", text: "2026-01-02T00:00:00Z", input: "2026-01-02"},
		{name: "timestamp minutes", typ: schema.Timestamp, in: day, text: "2026-01-02T07:30", input: "2026-01-02T07:30"},
		{name: "timestamp seconds", typ: schema.Timestamp, in: day.Add(15 * time.Second), text: "2026-01-02T07:30:15", input: "2026-01-02T07:30:15", step: 1},
		{name: "timestamp milliseconds", typ: schema.Timestamp, in: day.Add(15250 * time.Millisecond), text: "2026-01-02T07:30:15.250", input: "2026-01-02T07:30:15.250", step: 0.001},
		{name: "timestamp string", typ: schema.Timestamp, in: "2026-01-02T09:30:15.25Z", text: "2026-01-02T09:30:15.25Z", input: "2026-01-02T09:30:15.250", step: 0.001},
		{name: "json object", typ: schema.JSON, in: map[string]any{"a": 1.0, "b": []any{"x"}}, text: `{"a":1,"b":["x"]}`},
		{name: "json array", typ: schema.JSON, in: []any{1.0, "x"}, text: `[1,"x"]`},
		{name: "nil", typ: schema.String, in: nil, text: ""},
		{name: "nil bool", typ: schema.Bool, in: nil, text: ""},
		{name: "nil date pointer", typ: schema.Date, in: (*time.Time)(nil), text: ""},
		{name: "file", typ: schema.File, in: "uploads/a.png", text: "uploads/a.png"},
	} {
		text := Form(schema.Field{Name: "f", Type: tc.typ}, tc.in)
		if text != tc.text {
			t.Errorf("%s: Form = %q, want %q", tc.name, text, tc.text)
		}
		var input string
		switch tc.typ {
		case schema.Date:
			input = DateInput(text)
		case schema.Timestamp:
			input = TimestampInput(text)
		default:
			continue
		}
		if input != tc.input {
			t.Errorf("%s: input value = %q, want %q", tc.name, input, tc.input)
		}
		if step := TimestampStep(input); tc.typ == schema.Timestamp && step != tc.step {
			t.Errorf("%s: step = %v, want %v", tc.name, step, tc.step)
		}
	}
}
