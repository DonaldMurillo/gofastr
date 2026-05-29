package markdown

import (
	"strings"
	"testing"
)

// FuzzRenderHTML drives the markdown renderer with arbitrary input. Five
// shipped DoS findings on this surface (quadratic blockquotes, unbounded
// inline recursion, emphasis blowup) were all liveness failures — so the
// fuzz contract is: always terminate, never panic. Pathological inputs
// that re-introduce super-linear blowup surface as fuzz timeouts.
func FuzzRenderHTML(f *testing.F) {
	for _, s := range []string{
		"", "# hi", "plain text", "> > > > > x",
		strings.Repeat("> ", 500) + "x",
		strings.Repeat("*", 1000), strings.Repeat("[", 500) + "a",
		"[a](javascript:alert(1))", "```\n<script>\n```",
		"---\ntitle: x\n---\nbody", strings.Repeat("_a_", 500),
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Contract: returns, never panics. Result intentionally ignored.
		_ = RenderHTML(input)
	})
}
