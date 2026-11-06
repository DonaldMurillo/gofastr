package yaml

import "testing"

func TestIndentlessMappingLists(t *testing.T) {
	for _, input := range []string{
		"items:\n- a\n- b\nnext: done\n",
		"outer:\n  items:\n  - a\n  - b\n  next: done\n",
		"- items:\n  - a\n  - b\n  next: done\n",
	} {
		t.Run(input, func(t *testing.T) {
			node, err := Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			if node.Kind == List {
				node = node.List[0]
			} else if outer := node.Map["outer"]; outer != nil {
				node = outer
			}
			items := node.Map["items"]
			if items.Kind != List || len(items.List) != 2 || items.List[0].Value != "a" || items.List[1].Value != "b" || node.Map["next"].Value != "done" {
				t.Fatalf("list or sibling mapping lost: %#v", node)
			}
		})
	}
}
