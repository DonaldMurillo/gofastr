package appspec

import (
	"strings"
	"testing"
)

// A catalog block renders a framework/ui component that panics on an
// empty required field, so a block missing it passed validate and 500'd
// its whole screen. stat_card's label was the reported one (spelled
// text:, the entity_list key, it was dropped); a static card's value, a
// link_button's label and href, a hero CTA's href, a markdown block's
// text, a callout with neither title nor text and a pricing plan's CTA
// with no safe cta_href panic the same way. A number where the component
// takes text decoded as int64, read as "" and panicked too.
func TestCatalogBlockNeedsRequiredProps(t *testing.T) {
	const head = `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: dash
    route: /
    body:
`
	for _, tc := range []struct{ name, block, want string }{
		{"stat text not label", `      - kind: stat_card
        text: Posts
        props:
          source:
            entity: posts
            agg: count
`, "label"},
		{"stat no value", `      - kind: stat_card
        props:
          label: Uptime
`, "value"},
		{"stat number value", `      - kind: stat_card
        props:
          label: Uptime
          value: 42
`, "quote"},
		{"button no label", `      - kind: link_button
        props:
          href: /docs
`, "label"},
		{"button no href", `      - kind: link_button
        props:
          label: Docs
`, "href"},
		{"button bad href", `      - kind: link_button
        props:
          label: Docs
          href: "javascript:alert(1)"
`, "href"},
		{"hero cta no href", `      - kind: hero
        props:
          title: Hi
          cta_text: Start
`, "cta_href"},
		{"hero second no href", `      - kind: hero
        props:
          title: Hi
          secondary_text: Learn
`, "secondary_href"},
		{"hero number cta", `      - kind: hero
        props:
          title: Hi
          cta_text: 42
          cta_href: /start
`, "quote"},
		{"hero bool second", `      - kind: hero
        props:
          title: Hi
          secondary_text: true
          secondary_href: /learn
`, "quote"},
		{"hero number title", `      - kind: hero
        props:
          title: 2026
`, "quote"},
		{"hero number image", `      - kind: hero
        props:
          title: Hi
          image: 404
`, "quote"},
		{"hero bool media", `      - kind: hero
        props:
          title: Hi
          media: true
`, "quote"},
		{"page_header number title", `      - kind: page_header
        props:
          title: 2026
`, "quote"},
		{"page_header no title", `      - kind: page_header
        props:
          subtitle: Every plan
`, "title is required"},
		{"page_header blank title", `      - kind: page_header
        props:
          title: "  "
`, "title is required"},
		{"page_header number eyebrow", `      - kind: page_header
        props:
          title: Plans
          eyebrow: 3
`, "quote"},
		{"markdown no text", `      - kind: markdown
`, "text"},
		{"callout empty", `      - kind: callout
`, "callout block needs"},
		{"callout blank", `      - kind: callout
        text: "  "
        props:
          title: " "
`, "callout block needs"},
		{"callout number title", `      - kind: callout
        props:
          title: 404
`, "quote"},
		{"pricing cta no href", `      - kind: pricing
        props:
          plans:
            - name: Pro
              price: "$9"
              cta_text: Buy
`, "cta_href is required"},
		{"pricing bad href", `      - kind: pricing
        props:
          plans:
            - name: Pro
              price: "$9"
              cta_href: "javascript:alert(1)"
`, "not a safe link"},
		{"pricing number price", `      - kind: pricing
        props:
          plans:
            - name: Pro
              price: 9
`, "quote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := r5YAMLAppSpec(t, head+tc.block)
			if err == nil {
				t.Fatal("accepted; the component panics at render")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should name %s: %v", tc.want, err)
			}
		})
	}
	for _, ok := range []string{
		`      - kind: stat_card
        props:
          label: Posts
          source:
            entity: posts
            agg: count
`,
		`      - kind: stat_card
        props:
          label: Uptime
          value: "99.9%"
`,
		`      - kind: link_button
        props:
          label: Docs
          href: /docs
`,
		`      - kind: hero
        props:
          title: Hi
          cta_text: Start
          cta_href: /start
`,
		`      - kind: markdown
        text: "# Hi"
`,
		`      - kind: callout
        props:
          title: Heads up
`,
		`      - kind: callout
        text: Backups run nightly.
`,
		`      - kind: pricing
        props:
          plans:
            - name: Free
              price: "$0"
            - name: Pro
              price: "$9"
              cta_text: Buy
              cta_href: /buy
`,
	} {
		if err := r5YAMLAppSpec(t, head+ok); err != nil {
			t.Errorf("refused a complete block: %v\n%s", err, ok)
		}
	}
}
