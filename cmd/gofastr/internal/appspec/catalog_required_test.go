package appspec

import (
	"strings"
	"testing"
)

// A catalog block renders a framework/ui component that panics on an
// empty required field, so a block missing it passed validate and 500'd
// its whole screen. stat_card's label was the reported one (spelled
// text:, the entity_list key, it was dropped); a static card's value, a
// link_button's label and href, a hero CTA's href and a markdown block's
// text panic the same way. A number where the component takes text
// decoded as int64, read as "" and panicked too.
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
		{"markdown no text", `      - kind: markdown
`, "text"},
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
	} {
		if err := r5YAMLAppSpec(t, head+ok); err != nil {
			t.Errorf("refused a complete block: %v\n%s", err, ok)
		}
	}
}
