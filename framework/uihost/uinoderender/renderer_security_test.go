package uinoderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
)

// Property: every module-controlled string reaches the output entity-
// escaped. The attacker is a (lower-trust) process module returning a
// ui.node.v1 body over moduleproto; the host renders it to text/html
// (framework/processmodule_proxy.go decodeBody). The wire validator
// (core-ui/uinodev1) bounds and types every field, but several props are
// free-form within those bounds (heading text, badge text, gap, labels,
// values, cells, alt). This file pins that no such field can break out of
// a text node or an attribute value at ANY component surface, end to end
// through Validate + Render.
//
// Attack shapes (one happy-path plus distinct classes, per the
// property × surface rule — no 60-case matrix):
//   1. markup breakout:  <img src=x onerror=alert(1)>
//   2. attr-quote breakout: " onmouseover="alert(1)
//   3. script closer:    </script><script>alert(1)</script>
//
// Raw-breakout signal: a raw '<' starting the payload tag, or a raw '"'
// directly after onmouseover= (escaped output has &quot; there instead).
// Note `onerror=alert(1)` alone is NOT a signal: entity escaping leaves
// those bytes intact and inert inside a text node.

const (
	markupPayload = `<img src=x onerror=alert(1)>`
	quotePayload  = `" onmouseover="alert(1)`
	scriptPayload = `</script><script>alert(1)</script>`
)

// escapeSurfaces lists every free-text prop surface with a payload-carrying
// JSON tree for it. escapedMarkUp is the escaped prefix that MUST appear in
// the output, proving the payload was rendered (escaped), not silently
// dropped — the assertion that keeps this test non-vacuous. Empty means the
// surface's field never reaches the output verbatim (data-table column key
// is a lookup key only), so only the negative assertion applies.
var escapeSurfaces = []struct {
	name         string
	tree         func(payload string) string
	escapedMark  string
	skipPositive bool
}{
	{"heading.text", func(p string) string {
		return `{"component":"heading","props":{"level":2,"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"paragraph.text", func(p string) string {
		return `{"component":"paragraph","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"text.text", func(p string) string {
		return `{"component":"text","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"strong.text", func(p string) string {
		return `{"component":"strong","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"em.text", func(p string) string {
		return `{"component":"em","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"code.text", func(p string) string {
		return `{"component":"code","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"small.text", func(p string) string {
		return `{"component":"small","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"badge.text", func(p string) string {
		return `{"component":"badge","props":{"text":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"detail-list.label", func(p string) string {
		return `{"component":"detail-list","props":{"items":[{"label":"` + p + `","value":"v"}]}}`
	}, "&lt;img src=x", false},
	{"detail-list.value", func(p string) string {
		return `{"component":"detail-list","props":{"items":[{"label":"l","value":"` + p + `"}]}}`
	}, "&lt;img src=x", false},
	{"key-value.key", func(p string) string {
		return `{"component":"key-value","props":{"items":[{"key":"` + p + `","value":"v"}]}}`
	}, "&lt;img src=x", false},
	{"key-value.value", func(p string) string {
		return `{"component":"key-value","props":{"items":[{"key":"k","value":"` + p + `"}]}}`
	}, "&lt;img src=x", false},
	{"stat-card.label", func(p string) string {
		return `{"component":"stat-card","props":{"label":"` + p + `","value":"1"}}`
	}, "&lt;img src=x", false},
	{"stat-card.value", func(p string) string {
		return `{"component":"stat-card","props":{"label":"l","value":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"stat-card.unit", func(p string) string {
		return `{"component":"stat-card","props":{"label":"l","value":"1","unit":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"data-table.column.label", func(p string) string {
		return `{"component":"data-table","props":{"columns":[{"key":"k","label":"` + p + `"}],"rows":[{"cells":[{"text":"v"}]}]}}`
	}, "&lt;img src=x", false},
	{"data-table.column.key", func(p string) string {
		return `{"component":"data-table","props":{"columns":[{"key":"` + p + `","label":"L"}],"rows":[{"cells":[{"text":"v"}]}]}}`
	}, "", true}, // key is a lookup key; not emitted
	{"data-table.cell.text", func(p string) string {
		return `{"component":"data-table","props":{"columns":[{"key":"k","label":"L"}],"rows":[{"cells":[{"text":"` + p + `"}]}]}}`
	}, "&lt;img src=x", false},
	{"section.title", func(p string) string {
		return `{"component":"section","props":{"title":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"section.subtitle", func(p string) string {
		return `{"component":"section","props":{"title":"t","subtitle":"` + p + `"}}`
	}, "&lt;img src=x", false},
	{"card.title", func(p string) string {
		return `{"component":"card","props":{"title":"` + p + `"},"children":[{"component":"text","props":{"text":"b"}}]}`
	}, "&lt;img src=x", false},
	{"stack.gap", func(p string) string {
		return `{"component":"stack","props":{"gap":"` + p + `"},"children":[{"component":"text","props":{"text":"b"}}]}`
	}, "&lt;img src=x", false},
	{"cluster.gap", func(p string) string {
		return `{"component":"cluster","props":{"gap":"` + p + `"},"children":[{"component":"text","props":{"text":"b"}}]}`
	}, "&lt;img src=x", false},
	{"grid.gap", func(p string) string {
		return `{"component":"grid","props":{"gap":"` + p + `"},"children":[{"component":"text","props":{"text":"b"}}]}`
	}, "&lt;img src=x", false},
	{"button.label", func(p string) string {
		return `{"component":"button","props":{"label":"` + p + `"},"action_ref":"a"}`
	}, "&lt;img src=x", false},
	{"link.text", func(p string) string {
		return `{"component":"link","props":{"text":"` + p + `","to":"/ok"}}`
	}, "&lt;img src=x", false},
	{"image.alt", func(p string) string {
		return `{"component":"image","props":{"src":"/ok.png","alt":"` + p + `"}}`
	}, "&lt;img src=x", false},
}

func renderJSON(t *testing.T, jsonTree string) string {
	t.Helper()
	tt, err := uinodev1.Validate([]byte(jsonTree), uinodev1.DefaultLimits())
	if err != nil {
		t.Fatalf("Validate (payload tree must be schema-valid, the attack is in field CONTENT): %v", err)
	}
	h, err := New(staticResolver("/m/mod/rpc")).Render(tt)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return string(h)
}

func TestModuleStringsEscapeAtEveryProp(t *testing.T) {
	payloads := []struct {
		name    string
		payload string
	}{
		{"markup", markupPayload},
		{"quote-breakout", quotePayload},
		{"script-closer", scriptPayload},
	}
	for _, sf := range escapeSurfaces {
		for _, pl := range payloads {
			t.Run(sf.name+"/"+pl.name, func(t *testing.T) {
				out := renderJSON(t, sf.tree(escapeJSONString(pl.payload)))
				// Raw-breakout signals: an unescaped '<' opening the
				// payload tag, a raw script tag, or a raw quote right
				// after onmouseover= (escaped output has &quot;).
				for _, raw := range []string{"<img src=x", "<script>", `onmouseover="`} {
					if strings.Contains(out, raw) {
						t.Fatalf("raw %q reached output from %s:\n%s", raw, sf.name, out)
					}
				}
				// Non-vacuous for the MARKUP payload: its escaped prefix
				// must be present (the field rendered, escaped, not
				// silently dropped) unless the field is a lookup key
				// that never reaches the output. The other payloads
				// carry no <img, so only the negative applies.
				if pl.name == "markup" && !sf.skipPositive && !strings.Contains(out, sf.escapedMark) {
					t.Fatalf("escaped payload missing from %s output (field silently dropped?):\n%s", sf.name, out)
				}
			})
		}
	}
}

// TestURLPropsRejectSchemePayloads pins the URL boundary at this surface's
// entry: link.to and image.src are the two props that land in URL-bearing
// attributes (href, src). The wire validator must reject every scheme /
// off-origin / parser-confusion shape before Render is ever reached; the
// shapes that DO pass (host-relative paths) exclude whitespace and control
// bytes — but NOT double quotes: `/x"onerror="y` validates. The quote is
// neutralised downstream by attribute escaping, not by the URL charset,
// and TestURLPropsQuoteIsEscapedNotRejected below pins that rather than
// leaving it assumed. The entry in badURLs carrying a quote is rejected
// for its space, so it must not be read as evidence about quotes.
// Downstream of the validator, ui.Link additionally routes href through
// urlsafe.CleanAnchor and html.Image through urlsafe.ImageSource (pinned
// in their own packages); this test pins the first gate.
func TestURLPropsRejectSchemePayloads(t *testing.T) {
	badURLs := []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.example/x",  // scheme-relative
		"/\\evil.example/x", // backslash smuggling
		"/x\" onerror=\"y",  // rejected for the SPACE, not the quote
		"/x\tonerror=y",     // control byte
		"https://evil.example",
	}
	for _, field := range []string{"to", "src"} {
		for _, u := range badURLs {
			t.Run(field+"/"+u, func(t *testing.T) {
				var jsonTree string
				if field == "to" {
					jsonTree = `{"component":"link","props":{"text":"t","to":"` + escapeJSONString(u) + `"}}`
				} else {
					jsonTree = `{"component":"image","props":{"src":"` + escapeJSONString(u) + `","alt":"a"}}`
				}
				if _, err := uinodev1.Validate([]byte(jsonTree), uinodev1.DefaultLimits()); err == nil {
					t.Fatalf("validator accepted hostile URL %q for %s", u, field)
				}
			})
		}
	}
}

// A quote-bearing host-relative path passes the validator, so the guard
// that actually stops an attribute breakout is the escaping in the render
// layer. Pinning it here means the two exemptions that lean on "the URL
// props are guarded" name a defence that exists: without escaping, this
// href would close its attribute and open an event handler.
func TestURLPropsQuoteIsEscapedNotRejected(t *testing.T) {
	const hostile = `/x"onerror="y`
	tree := `{"component":"link","props":{"text":"t","to":"` + escapeJSONString(hostile) + `"}}`

	if _, err := uinodev1.Validate([]byte(tree), uinodev1.DefaultLimits()); err != nil {
		t.Fatalf("premise changed: the validator now rejects %q (%v). "+
			"If the charset excludes quotes, simplify this test and the comment above.", hostile, err)
	}
	out := renderJSON(t, tree)
	if strings.Contains(out, `"onerror="`) {
		t.Fatalf("SECURITY: the quote reached the attribute raw, closing it:\n%s", out)
	}
	if !strings.Contains(out, "&#34;") && !strings.Contains(out, "&quot;") {
		t.Fatalf("quote neither escaped nor rejected — the payload vanished, so this proves nothing:\n%s", out)
	}
	// The two assertions above are blind to NON-uniform escaping: an
	// escaper that escaped one occurrence and not the other would satisfy
	// both. Today the registry's well-formedness gate rejects the broken
	// markup that results, but that is a different layer's guarantee.
	// Pinning the exact attribute keeps this test self-sufficient.
	if want := `href="/x&quot;onerror=&quot;y"`; !strings.Contains(out, want) {
		t.Fatalf("href not escaped exactly as expected:\n got: %s\nwant substring: %s", out, want)
	}
}

// escapeJSONString encodes s as a JSON string body (quotes + backslashes
// escaped) so hostile URL spellings survive the JSON layer byte-for-byte.
func escapeJSONString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
