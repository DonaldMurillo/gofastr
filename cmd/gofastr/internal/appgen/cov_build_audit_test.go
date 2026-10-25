package appgen

import (
	"testing"
)

// ── generate_typed.go columnConstructor ───────────────────────────────

func TestColumnConstructorAll(t *testing.T) {
	cases := map[string]string{
		"int": "framework.NewIntColumn", "integer": "framework.NewIntColumn",
		"float": "framework.NewFloatColumn", "decimal": "framework.NewFloatColumn",
		"bool": "framework.NewBoolColumn", "timestamp": "framework.NewTimestampColumn",
		"date": "framework.NewTimestampColumn", "uuid": "framework.NewUUIDColumn",
		"relation": "framework.NewUUIDColumn", "string": "framework.NewStringColumn",
		"json": "framework.NewStringColumn",
	}
	for in, want := range cases {
		if got := columnConstructor(in); got != want {
			t.Errorf("columnConstructor(%q)=%q want %q", in, got, want)
		}
	}
}
