package appspec

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// decodeBlocks reads a catalog kind spelled `type:` as the block's kind,
// at every depth, so each reader of a block sees one field. The sugar
// types (heading, paragraph, link, section) and the entity blocks keep
// `type:`, and a `kind:` beside a `type:` wins as written. Before, the
// validator read `type: card` as a card and the emitter did not.
func TestDecodeReadsTypeSpelledCatalogKindAsKind(t *testing.T) {
	node, err := coreyaml.Parse(`
app:
  name: Demo
  module: example.com/demo
screens:
  - name: home
    route: /
    body:
      - type: card
        children:
          - type: text_field
            props:
              name: q
              label: Query
          - type: paragraph
            text: hello
      - type: section
        text: plain
      - type: heading
        text: Title
      - type: Stat_Card
        props:
          label: Posts
      - type: card
        kind: div
`)
	if err != nil {
		t.Fatal(err)
	}
	bp, err := DecodeAppSpec(node)
	if err != nil {
		t.Fatal(err)
	}
	body := bp.Screens[0].Body
	got := [][2]string{
		{body[0].Type, body[0].Kind},
		{body[0].Children[0].Type, body[0].Children[0].Kind},
		{body[0].Children[1].Type, body[0].Children[1].Kind},
		{body[1].Type, body[1].Kind},
		{body[2].Type, body[2].Kind},
		{body[3].Type, body[3].Kind},
		{body[4].Type, body[4].Kind},
	}
	want := [][2]string{
		{"", "card"},
		{"", "text_field"},
		{"paragraph", ""},
		{"section", ""},
		{"heading", ""},
		{"", "Stat_Card"},
		{"card", "div"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d decoded as type=%q kind=%q, want type=%q kind=%q", i, got[i][0], got[i][1], want[i][0], want[i][1])
		}
	}
}
