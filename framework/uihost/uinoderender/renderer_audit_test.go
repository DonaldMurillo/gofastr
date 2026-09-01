package uinoderender

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Audit additions (issue #136 slice). The existing
// renderer_security_test.go pins text-node and attribute-VALUE escaping
// end-to-end for every free-text prop, and renderer_escape_inventory_
// test.go pins that the surface table matches the uinodev1 schema.
// These pin the surfaces neither file reaches:
//
//   - image.src with an embedded quote: IsValidHostRelative accepts it
//     (it only rejects space/control/backslash), so the quote must be
//     neutralised by the render layer — pinned for link.to by
//     TestURLPropsQuoteIsEscapedNotRejected but never for image.src
//   - attribute-NAME injection: no raw `on…="…"` / `on…='…'` attribute
//     syntax anywhere in the output, across every free-text surface and
//     attribute-shaped payloads (the existing suite only tests quote
//     breakouts with one spelling per surface)
//   - no `style=` attribute anywhere in the layout chain (the CSS
//     injection surface: gap/grid bytes must never reach a style context)
//   - the downstream backstop render.Attr rejects MIXED-CASE event
//     handler names (OnClick), the premise the known "noderender on*
//     filter is lowercase-only" non-finding leans on — re-derived here
//     empirically, once, and moved on from
//   - FINDING (characterization): a hand-built (unvalidated) tree with
//     nil or wrong-typed Props PANICS in renderNode's type assertions,
//     contradicting renderDataTable's own "a hand-built (unvalidated)
//     Tree can never crash the renderer" guard comment. The panic is
//     pinned with recover() so the suite stays green; when the renderer
//     is hardened, flip this test to assert the error return instead.

// TestImageSrcQuoteIsEscapedNotRejected is the image.src twin of
// TestURLPropsQuoteIsEscapedNotRejected. A quote-bearing host-relative
// path passes the wire validator, so the guard that stops an attribute
// breakout is escaping in the render layer. The exact-attribute pin
// keeps this self-sufficient against non-uniform escaping (an escaper
// that escaped one occurrence and not the other would pass a naive
// contains-check).
func TestImageSrcQuoteIsEscapedNotRejected(t *testing.T) {
	const hostile = `/x"onerror="y`
	tree := `{"component":"image","props":{"src":"` + escapeJSONString(hostile) + `","alt":"a"}}`

	if _, err := uinodev1.Validate([]byte(tree), uinodev1.DefaultLimits()); err != nil {
		t.Fatalf("premise changed: the validator now rejects %q (%v). "+
			"If the charset excludes quotes, simplify this test.", hostile, err)
	}
	out := renderJSON(t, tree)
	if strings.Contains(out, `"onerror="`) {
		t.Fatalf("SECURITY: the quote reached the src attribute raw, closing it:\n%s", out)
	}
	if !strings.Contains(out, "&quot;") {
		t.Fatalf("quote neither escaped nor rejected — the payload vanished, so this proves nothing:\n%s", out)
	}
	if want := `src="/x&quot;onerror=&quot;y"`; !strings.Contains(out, want) {
		t.Fatalf("src not escaped exactly as expected:\n got: %s\nwant substring: %s", out, want)
	}
}

// rawAttrSyntax matches a raw event-handler attribute: whitespace, on…,
// =, then a quote — with optional whitespace around the =, which HTML
// accepts (`onload = "x"` executes exactly like `onload="x"`). Escaped
// output never has a raw quote after an = in attribute position; the
// same bytes inside a TEXT node are inert and never match because their
// quotes are entity-escaped.
var rawAttrSyntax = regexp.MustCompile(`\son[a-zA-Z]+\s*=\s*("|')`)

// TestNoRawEventHandlerAttributesAcrossSurfaces sweeps every free-text
// prop surface with attribute-NAME-shaped payloads. None of the
// module-controlled strings can become an attribute name (the renderer
// assigns every attribute name itself), so this pins that property
// against drift: if a future component forwards a prop into an
// attribute-name position, one of these payloads produces raw
// `onload="` syntax and this fails.
func TestNoRawEventHandlerAttributesAcrossSurfaces(t *testing.T) {
	payloads := []string{
		`x onload="alert(1)"`,
		` onload="alert(1)"`,
		`onload='alert(1)'`,
		`" onclick="alert(1)`,
		`tabindex="1" onfocus="alert(1)`,
		`x" onload = "alert(1)`,
	}
	for _, sf := range escapeSurfaces {
		for _, p := range payloads {
			t.Run(sf.name, func(t *testing.T) {
				out := renderJSON(t, sf.tree(escapeJSONString(p)))
				if loc := rawAttrSyntax.FindString(out); loc != "" {
					t.Fatalf("SECURITY: raw attribute syntax %q reached output from %s:\n%s", loc, sf.name, out)
				}
				// Non-vacuity for a known-rendered surface: the payload
				// must be IN the output, entity-escaped, not dropped.
				if sf.name == "heading.text" && strings.Contains(p, `onload="`) &&
					!strings.Contains(out, `onload=&quot;alert(1)&quot;`) {
					t.Fatalf("payload vanished from heading.text output — sweep is vacuous for it:\n%s", out)
				}
			})
		}
	}
}

// TestLayoutChainEmitsNoStyleAttribute renders every layout component
// with gap/columns set and asserts no style attribute appears anywhere.
// This is the CSS-injection surface: gap and column-count are the only
// module-controlled bytes that could plausibly reach a style context,
// and the design system routes them through class names (gap) and the
// data-min custom property (columns) instead. If any component in the
// chain ever starts emitting style=, CSS injection needs re-auditing.
func TestLayoutChainEmitsNoStyleAttribute(t *testing.T) {
	tree := `{"component":"stack","props":{"gap":"md"},"children":[` +
		`{"component":"grid","props":{"columns":3,"gap":"sm"},"children":[` +
		`{"component":"cluster","props":{"gap":"xs"},"children":[` +
		`{"component":"section","props":{"title":"T","subtitle":"S"},"children":[` +
		`{"component":"card","props":{"title":"C","elevation":"low"},"children":[` +
		`{"component":"text","props":{"text":"b"}}]}]}]}]}]}`
	tt, err := uinodev1.Validate([]byte(tree), uinodev1.DefaultLimits())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out, err := New(staticResolver("/m/r")).Render(tt)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "style=") {
		t.Fatalf("layout chain emitted a style attribute (CSS injection surface):\n%s", out)
	}
}

// TestRenderAttrRejectsMixedCaseEventHandlers is the empirical
// verification of the premise behind the previously-filed (and
// rejected) "noderender on* filter is lowercase-only" finding: the
// shared sink render.Attr rejects on* attribute names
// CASE-INSENSITIVELY, so OnClick cannot ride through as an attribute
// name even where an upstream filter only checks lowercase. uinoderender
// itself never forwards module-controlled attribute names at all (every
// attribute in its output has a host-assigned name), so this backstop
// is defence-in-depth for the whole render stack, re-derived once here.
func TestRenderAttrRejectsMixedCaseEventHandlers(t *testing.T) {
	for _, name := range []string{"OnClick", "ONLOAD", "onerror", "onMouseOver"} {
		if got := render.Attr(name, "alert(1)"); got != "" {
			t.Fatalf("render.Attr accepted event-handler attribute name %q: %q", name, got)
		}
	}
}

// TestHandBuiltTreeWithoutPropsPanics is a CHARACTERIZATION test for a
// documented audit finding, not a desired property: renderNode's
// `n.Props.(uinodev1.XProps)` assertions panic on a hand-built
// (unvalidated) tree whose Props is nil or the wrong concrete type,
// which contradicts renderDataTable's guard comment claiming "a
// hand-built (unvalidated) Tree can never crash the renderer". Today
// the only production path (processmodule_proxy decodeBody) validates
// before rendering, so the panic is unreachable in production; this
// test pins the defect so it cannot silently become load-bearing. When
// the assertions are hardened to fail closed with an error, DELETE the
// recover-based assertions below and assert the error return instead.
func TestHandBuiltTreeWithoutPropsPanics(t *testing.T) {
	cases := []struct {
		name string
		root uinodev1.Node
	}{
		{"nil props", uinodev1.Node{Component: uinodev1.CompHeading}},
		{"nil props layout", uinodev1.Node{Component: uinodev1.CompStack}},
		{"wrong props type", uinodev1.Node{Component: uinodev1.CompHeading, Props: uinodev1.ParagraphProps{Text: "x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("renderNode no longer panics on %s — the characterization is stale: "+
						"replace this test with an assertion that Render returns an error", tc.name)
				}
			}()
			r := New(nil)
			_, _ = r.Render(&uinodev1.Tree{Root: tc.root})
		})
	}
}
