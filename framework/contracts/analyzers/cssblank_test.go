package analyzers

import (
	"strings"
	"testing"
)

// The whole risk in the string-aware pre-pass is that a blanked span
// changes length or drops a newline, which silently misattributes every
// later finding's line number. Assert the invariant directly.
func TestBlankPreservesOffsetsAndLines(t *testing.T) {
	cases := []string{
		"",
		".a{color:red}\n",
		"/* c */ .a{}\n",
		"/* unterminated\n.a{color:var(--x)}\n",
		".x::after{content:\"/*\"}\n.y{color:var(--z)}\n",
		".x::after{content:'a\\'b'}\n",
		".x::after{content:\"a\\\nb\"}\n",
		".x::after{content:\"raw\nnewline\"}\n",
		".x{content:\"hi\";color:var(--q)}\n",
		"a\\",
		"/*/*nested-looking*/ .a{}\n",
		"\"unterminated string",
	}
	for _, in := range cases {
		out := blankCSSCommentsAndStrings(in)
		if len(out) != len(in) {
			t.Errorf("length changed: %d -> %d for %q", len(in), len(out), in)
		}
		if a, b := strings.Count(in, "\n"), strings.Count(out, "\n"); a != b {
			t.Errorf("newline count changed: %d -> %d for %q", a, b, in)
		}
		for i := range in {
			if in[i] == '\n' && out[i] != '\n' {
				t.Errorf("newline at %d moved for %q -> %q", i, in, out)
			}
		}
	}
}
