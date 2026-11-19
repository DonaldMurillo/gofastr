package uihost

import (
	"strings"
	"testing"
)

// The head scrub read a link's rel and href with a regex that also
// matched text inside another attribute's quoted value, so a fake
// `rel=icon href=''` hidden in data-* hid the real rel or href. And it
// stripped handlers and autofocus only after a space or a slash, while a
// browser also starts a new attribute right after a closing quote.

var headScrubAttrBypass = []struct {
	name, payload, forbidden string
}{
	{"preload-hidden-behind-fake-rel", `<link data-note=" rel=icon href='' " rel="preload" as="image" href="/hero.jpg">`, `rel="preload"`},
	{"preload-hidden-single-quotes", `<link data-note=' rel=icon ' rel='preload' href='/a.js'>`, `rel='preload'`},
	{"modulepreload-uppercase", `<link data-x=" rel=icon " REL="ModulePreload" href="/m.js">`, `modulepreload`},
	{"unsafe-href-behind-fake-href", `<link data-n=" href='' " rel="stylesheet" href="javascript:alert(1)">`, `javascript:`},
	{"handler-after-closing-quote", `<link rel="stylesheet"onload="alert(1)" href="/a.css">`, `onload=`},
	{"handler-after-single-quote", `<meta name='x'onfocus='alert(1)'>`, `onfocus=`},
	{"autofocus-after-closing-quote", `<meta name="x"autofocus>`, `autofocus`},
}

func TestHeadScrubReadsRealAttributesOnly(t *testing.T) {
	for _, tc := range headScrubAttrBypass {
		t.Run(tc.name, func(t *testing.T) {
			for _, sink := range []struct{ name, page string }{
				{"WithHeadHTML", renderHeadPage(t, WithHeadHTML(tc.payload))},
				{"SEOScreen.HeadHTML", renderScreenHeadPage(t, tc.payload)},
				{"stripInlineScripts", stripInlineScripts(tc.payload)},
			} {
				if strings.Contains(strings.ToLower(sink.page), strings.ToLower(tc.forbidden)) {
					t.Errorf("SECURITY: [uihost] %s: scrub of %q kept %q", sink.name, tc.payload, tc.forbidden)
				}
			}
		})
	}
}

// TestHeadScrubKeepsSafeLinks: reading real attributes must not drop a
// safe link, including one whose data-* text mentions preload.
func TestHeadScrubKeepsSafeLinks(t *testing.T) {
	for _, keep := range []string{
		`<link rel="icon" href="/favicon.png">`,
		`<link data-note="rel=preload" rel="stylesheet" href="/a.css">`,
		`<link rel="alternate" hreflang="es" href="https://example.com/es/">`,
		`<link rel="manifest" href="/app.webmanifest">`,
	} {
		if got := stripInlineScripts(keep); got != keep {
			t.Errorf("a safe link was changed:\n in: %s\nout: %s", keep, got)
		}
	}
}
