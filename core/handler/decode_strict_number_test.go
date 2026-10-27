package handler

import (
	"encoding/json"
	"testing"
)

// UseNumber keeps an integer past 2^53 exact; without it the decode is
// unchanged for existing callers (float64, rounded).
func TestUseNumberKeepsIntegerDigits(t *testing.T) {
	body := []byte(`{"tenant":9007199254740993}`)

	var exact map[string]any
	if err := UnmarshalStrict(body, &exact, UseNumber()); err != nil {
		t.Fatal(err)
	}
	if n, ok := exact["tenant"].(json.Number); !ok || n.String() != "9007199254740993" {
		t.Fatalf("UseNumber tenant = %#v, want json.Number 9007199254740993", exact["tenant"])
	}

	var plain map[string]any
	if err := UnmarshalStrict(body, &plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["tenant"].(float64); !ok {
		t.Fatalf("default tenant = %T, want float64 (existing callers unchanged)", plain["tenant"])
	}
}

// The option changes number decoding only: the key rules still refuse.
func TestUseNumberKeepsKeyRules(t *testing.T) {
	var dst map[string]any
	if err := UnmarshalStrict([]byte(`{"a":1,"a":2}`), &dst, UseNumber()); err == nil {
		t.Fatal("duplicate key accepted under UseNumber")
	}
}
