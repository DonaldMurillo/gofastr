package yaml

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Double-quoted scalars decode by the YAML 1.2 escape table (section
// 5.7). They used to go through strconv.Unquote, which reads Go syntax:
// "\xe9" came back as the lone byte 0xE9, invalid UTF-8, where YAML
// means "é", and Go's octal "\101" read as "A" where YAML has no such
// escape. Both were wrong values with no error.
func TestDoubleQuotedEscapesFollowYAML(t *testing.T) {
	cases := map[string]string{
		`"caf\xe9"`:              "café",
		`"\x41\u00e9\U0001F600"`: "Aé😀",
		`"a\/b"`:                 "a/b",
		`"\e[0m"`:                "\x1b[0m",
		`"\N\_\L\P"`:             "\u0085   ",
		`"\0\a\b\t\n\v\f\r"`:     "\x00\a\b\t\n\v\f\r",
		`"\ x"`:                  " x",
		`"q\"q\\"`:               `q"q\`,
		`"plain é"`:              "plain é",
	}
	for in, want := range cases {
		n, err := Parse("k: " + in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		got, _ := n.Map["k"].Value.(string)
		if got != want {
			t.Errorf("%s = %q, want %q", in, got, want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s decoded to invalid UTF-8 %q", in, got)
		}
	}
}

func TestDoubleQuotedRefusesNonYAMLEscapes(t *testing.T) {
	for _, in := range []string{
		`"\101"`,       // Go octal
		`"\'"`,         // Go rune-literal escape
		`"\q"`,         // undefined
		`"\x4"`,        // short hex
		`"\u12"`,       // short hex
		`"\uD800"`,     // lone surrogate
		`"\U00110000"`, // past U+10FFFF
		`"\x+1"`,       // sign is not a hex digit
		`"a\"`,         // escaped closing quote: unterminated
	} {
		if n, err := Parse("k: " + in); err == nil {
			t.Errorf("%s accepted as %q, want an error", in, n.Map["k"].Value)
		} else if strings.Contains(err.Error(), "invalid syntax") {
			t.Errorf("%s: error %q names no YAML rule", in, err)
		}
	}
}
