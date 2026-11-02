package yaml

import (
	"strings"
	"testing"
)

// Plain scalars resolve by the YAML 1.2 core schema (section 10.3.2).
// The parser used to borrow Go's spellings instead: strconv.ParseFloat
// reads hex floats (0x1.8p1 as 3), strings.ToLower made tRuE a boolean,
// and an out-of-range float fell back to a string. Each of those reads
// the same file differently from every other YAML tool.
func TestCoreSchemaScalarResolution(t *testing.T) {
	cases := map[string]any{
		"true":     true,
		"True":     true,
		"TRUE":     true,
		"false":    false,
		"False":    false,
		"FALSE":    false,
		"null":     nil,
		"Null":     nil,
		"NULL":     nil,
		"~":        nil,
		"tRuE":     "tRuE",
		"fAlSe":    "fAlSe",
		"nUlL":     "nUlL",
		"0x1.8p1":  "0x1.8p1",
		"0x1ep1":   "0x1ep1",
		"-0x1.0p0": "-0x1.0p0",
		"1.5":      1.5,
		"-2.5e3":   -2500.0,
		"1e3":      1000.0,
		".5":       0.5,
		"1.":       1.0,
		"Infinity": "Infinity",
		"1_000.5":  "1_000.5",
		"42":       int64(42),
	}
	for raw, want := range cases {
		node, err := Parse("v: " + raw + "\n")
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if got := node.Map["v"].Value; got != want {
			t.Errorf("v: %s = %#v, want %#v", raw, got, want)
		}
	}
}

// A float the core schema names but float64 cannot hold is refused, not
// read as a string or as +Inf: a bound like max: 1e999 is a typo, and an
// infinite one passes every range check downstream.
func TestCoreSchemaOutOfRangeFloatRefused(t *testing.T) {
	for _, raw := range []string{"1e999", "-1e999", "[1, 1e400]"} {
		_, err := Parse("v: " + raw + "\n")
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("v: %s: err = %v, want an out-of-range refusal", raw, err)
		}
	}
	// A quoted spelling stays a string.
	node, err := Parse(`v: "1e999"` + "\n")
	if err != nil || node.Map["v"].Value != "1e999" {
		t.Errorf(`quoted "1e999" = %v, %v`, node, err)
	}
}

// A local tag (!foo) used to read as the plain string "!foo bar". This
// parser resolves no tags, so it refuses one the way it refuses !!str.
func TestLocalTagRefused(t *testing.T) {
	for _, in := range []string{"v: !foo bar\n", "v: !foo\n", "v: !\n", "v: [a, !x b]\n", "v:\n  - !env HOME\n"} {
		_, err := Parse(in)
		if err == nil || !strings.Contains(err.Error(), "tag") {
			t.Errorf("Parse(%q) err = %v, want a tag refusal", in, err)
		}
	}
	node, err := Parse(`v: "!foo"` + "\n")
	if err != nil || node.Map["v"].Value != "!foo" {
		t.Errorf(`quoted "!foo" = %v, %v`, node, err)
	}
}
