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

func TestCoreSchemaIntegerResolution(t *testing.T) {
	cases := map[string]any{
		"0x10":                     int64(16),
		"0xAf":                     int64(175),
		"0o10":                     int64(8),
		"0x7fffffffffffffff":       int64(1<<63 - 1),
		"0o777777777777777777777":  int64(1<<63 - 1),
		"010":                      int64(10),
		"+010":                     int64(10),
		"-010":                     int64(-10),
		"0x8000000000000000":       "0x8000000000000000",
		"0o1000000000000000000000": "0o1000000000000000000000",
		"0x":                       "0x",
		"0o":                       "0o",
		"0xg":                      "0xg",
		"0o8":                      "0o8",
		"0x+10":                    "0x+10",
		"0o-10":                    "0o-10",
		"-0x10":                    "-0x10",
		"+0x1F":                    "+0x1F",
		"+0o10":                    "+0o10",
		"0X10":                     "0X10",
		"0O10":                     "0O10",
		"0b10":                     "0b10",
		"1_000":                    "1_000",
		"0x1_0":                    "0x1_0",
		"0o1_0":                    "0o1_0",
		`"0x10"`:                   "0x10",
		"'0o10'":                   "0o10",
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			node, err := Parse("v: " + raw + "\n")
			if err != nil {
				t.Fatal(err)
			}
			if got := node.Map["v"].Value; got != want {
				t.Errorf("got %T(%v), want %T(%v)", got, got, want, want)
			}
		})
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

func TestNeedsQuote(t *testing.T) {
	for _, s := range []string{
		"True", "FALSE", "Null", "~", "+010", "0x10", "0o10", ".5", "1e3", "1e999",
		"", " leading", "trailing ", "interior\ttab", "first\nsecond", "a\x7fb", "a\xffb",
		"#comment", "a #comment", "a: b", "trailing:", "- list", "a,b", "a[b]", "a{b}", "it's", `"quote"`,
	} {
		if !NeedsQuote(s) {
			t.Errorf("NeedsQuote(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"tRuE", "yes", "open", "+0x1F", "1_000", "0x1.8p1", "1_000.5", "9:30", "café", "a#b",
	} {
		if NeedsQuote(s) {
			t.Errorf("NeedsQuote(%q) = true, want false", s)
		}
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
