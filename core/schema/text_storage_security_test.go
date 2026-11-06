package schema

import (
	"encoding/json"
	"testing"
)

func TestTextRefusesUnstorableBytes(t *testing.T) {
	for _, kind := range []FieldType{String, Text} {
		field := Field{Name: "value", Type: kind}
		for _, value := range []string{"a\x00b", "\x00", "bad\xff", "short\xe2\x82"} {
			for _, result := range []ValidationResult{ValidateAll(Schema{Fields: []Field{field}}, map[string]any{"value": value}), ValidatePartial(Schema{Fields: []Field{field}}, map[string]any{"value": value})} {
				if result.Valid || len(result.Errors["value"]) != 1 || result.Errors["value"][0].Code() != "invalid_text" {
					t.Errorf("type %v accepts %q or loses field reason: %+v", kind, value, result)
				}
			}
		}
		for _, value := range []string{"", "valid café 日本語", "line\n\ttwo", "replacement \ufffd"} {
			if err := Validate(field, value); err != nil {
				t.Errorf("type %v refuses valid text %q: %v", kind, value, err)
			}
		}
	}
}

func TestJSONRefusesUnstorableStrings(t *testing.T) {
	field := Field{Name: "value", Type: JSON}
	for _, value := range []any{
		`{"nested":["a\u0000b"]}`, `{"a\u0000b":1}`, `"\u0000"`, "{\"value\":\"\xff\"}",
		map[string]any{"nested": []any{map[string]any{"value": "a\x00b"}}},
		map[string]any{"key\x00": true},
		// Byte slices bind as JSON text, so they are read as text, not
		// as the base64 string json.Marshal would make of them.
		[]byte(`{"nested":["a\u0000b"]}`), []byte(`{"a\u0000b":1}`), []byte("{\"value\":\"bad\xff\"}"),
		json.RawMessage(`{"nested":["a\u0000b"]}`), json.RawMessage("{\"value\":\"bad\xff\"}"),
	} {
		if err := Validate(field, value); err == nil || AsViolation(err).Code() != "invalid_text" {
			t.Errorf("JSON accepts unstorable strings in %q: %v", value, err)
		}
	}
	for _, value := range []any{`{"literal":"\\u0000","value":"café"}`, `{"number":1e9999}`, map[string]any{"value": "日本語"}, []byte(`{"value":"café"}`), json.RawMessage(`[1,"x"]`)} {
		if err := Validate(field, value); err != nil {
			t.Errorf("valid JSON %v refused: %v", value, err)
		}
	}
}
