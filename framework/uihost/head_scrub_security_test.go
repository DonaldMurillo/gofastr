package uihost

import (
	"strings"
	"testing"
)

// The head scrub (WithHeadHTML, SEOScreen.HeadHTML) removes bytes, and
// removing an inner match splices its neighbours together. One pass of
// it left the very tag it exists to remove: `<scr<script></script>ipt>`
// became `<script>`, `ref/autofocusresh` became `refresh`. The scrub now
// runs to a fixpoint, and HTML that is still changing at the pass cap is
// dropped whole.

var headScrubReassembly = []struct {
	name, payload, forbidden string
}{
	{"script-split-by-script", `<scr<script></script>ipt>alert(1)</scr<script></script>ipt>`, `<script>alert(1)</script>`},
	{"script-split-by-svg", `<scr<svg/>ipt>alert(1)</scr<svg/>ipt>`, `<script>alert(1)</script>`},
	{"script-split-by-img", `<scr<img src=x>ipt>alert(1)</script>`, `<script>alert(1)`},
	{"meta-refresh-split-by-autofocus", `<meta http-equiv="ref/autofocusresh" content="0;url=https://evil.example">`, `http-equiv="refresh"`},
	{"handler-split-by-autofocus", `<input/o/autofocusnfocus=alert(1)>`, `onfocus=`},
	{"iframe-split-by-style", `<if<style></style>rame src=https://evil.example></iframe>`, `<iframe`},
}

func TestHeadScrubDoesNotReassembleTags(t *testing.T) {
	for _, tc := range headScrubReassembly {
		t.Run(tc.name, func(t *testing.T) {
			for _, sink := range []struct{ name, page string }{
				{"WithHeadHTML", renderHeadPage(t, WithHeadHTML(tc.payload))},
				{"SEOScreen.HeadHTML", renderScreenHeadPage(t, tc.payload)},
				{"stripInlineScripts", stripInlineScripts(tc.payload)},
			} {
				if strings.Contains(strings.ToLower(sink.page), strings.ToLower(tc.forbidden)) {
					t.Errorf("SECURITY: [uihost] %s: scrub of %q emitted %q: removing an inner match reassembled the outer one",
						sink.name, tc.payload, tc.forbidden)
				}
			}
		})
	}
}

// nestedRefresh nests the autofocus split depth times; each level costs
// the scrub one more pass.
func nestedRefresh(depth int) string {
	m := `<meta http-equiv="ref/autofocusresh" content="0;url=https://evil.example">`
	for range depth {
		m = strings.Replace(m, "/autofocus", "/auto/autofocusfocus", 1)
	}
	return m
}

// HTML nested past the pass cap is dropped whole, never returned half
// scrubbed; honest head HTML passes through untouched.
func TestHeadScrubFailsClosedAtPassCap(t *testing.T) {
	deep := nestedRefresh(headScrubMaxPasses+4) + `<meta name="description" content="kept?">`
	if got := stripInlineScripts(deep); got != "" {
		t.Errorf("SECURITY: [uihost] head HTML still changing at the pass cap came back as %q; want it dropped", got)
	}
	page := renderHeadPage(t, WithHeadHTML(deep))
	if strings.Contains(page, "kept?") || strings.Contains(page, "evil.example") {
		t.Errorf("SECURITY: [uihost] WithHeadHTML past the pass cap reached the page")
	}

	// Nesting inside the cap converges and scrubs.
	if got := stripInlineScripts(nestedRefresh(2)); strings.Contains(got, "refresh") {
		t.Errorf("SECURITY: [uihost] nested refresh inside the cap survived: %q", got)
	}

	honest := `<meta name="description" content="Plain page"><link rel="canonical" href="https://example.com/x"><meta property="og:title" content="X">`
	if got := stripInlineScripts(honest); got != honest {
		t.Errorf("honest head HTML changed: %q -> %q", honest, got)
	}
}
