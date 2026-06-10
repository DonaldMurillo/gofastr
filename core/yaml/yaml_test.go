package yaml

import "testing"

func TestParseSubsetMapsListsAndScalars(t *testing.T) {
	node, err := Parse(`
app:
  name: "Demo # not a comment"
  enabled: true
  retries: 3
entities:
  - name: posts
    fields:
      - name: title
        type: string
        values: [draft, "published"]
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if node.Kind != Map {
		t.Fatalf("root kind = %v, want Map", node.Kind)
	}
	app := node.Map["app"]
	if got := app.Map["name"].Value; got != "Demo # not a comment" {
		t.Fatalf("app.name = %v", got)
	}
	if got := app.Map["enabled"].Value; got != true {
		t.Fatalf("app.enabled = %v", got)
	}
	if got := app.Map["retries"].Value; got != int64(3) {
		t.Fatalf("app.retries = %#v", got)
	}
	values := node.Map["entities"].List[0].Map["fields"].List[0].Map["values"].List
	if len(values) != 2 || values[0].Value != "draft" || values[1].Value != "published" {
		t.Fatalf("values = %#v", values)
	}
}

func TestParseSubsetScalarVariants(t *testing.T) {
	node, err := Parse(`
single: 'it''s fine'
double: "line\nbreak"
null_a: null
null_b: ~
float: 1.25
negative: -7
commented: value # removed
hash: "value # kept"
empty_list: []
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cases := map[string]any{
		"single":    "it's fine",
		"double":    "line\nbreak",
		"null_a":    nil,
		"null_b":    nil,
		"float":     1.25,
		"negative":  int64(-7),
		"commented": "value",
		"hash":      "value # kept",
	}
	for key, want := range cases {
		if got := node.Map[key].Value; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
	if got := len(node.Map["empty_list"].List); got != 0 {
		t.Fatalf("empty_list len = %d", got)
	}
}

func TestParseSubsetNestedListItems(t *testing.T) {
	node, err := Parse(`
screens:
  -
    name: home
    body:
      -
        type: heading
        text: Home
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := node.Map["screens"].List[0].Map["body"].List[0].Map
	if body["type"].Value != "heading" || body["text"].Value != "Home" {
		t.Fatalf("body = %#v", body)
	}
}

func TestFlowMapErrMentionsBlockStyle(t *testing.T) {
	_, err := Parse(`
entities:
  - name: Article
    fields:
      - { name: title, type: string }
`)
	if err == nil {
		t.Fatal("Parse returned nil error for flow-mapping list item")
	}
	want := `yaml:5:9: flow mapping "{ ... }" is not supported; use block style (one "key: value" per indented line)`
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
}

func TestFlowMapValueErrMatches(t *testing.T) {
	_, err := Parse("fields: { name: title }\n")
	if err == nil {
		t.Fatal("Parse returned nil error for flow-mapping value")
	}
	want := `yaml:1:9: flow mapping "{ ... }" is not supported; use block style (one "key: value" per indented line)`
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
}

func TestParseSubsetRejectsUnsupportedSyntax(t *testing.T) {
	cases := []string{
		"app: {name: demo}\n",
		"items:\n  - [a: b, c]\n",
		"app:\n\tname: demo\n",
		"values: [one, \"two]\n",
		"name: \"unterminated\n",
		"bad scalar: value: nested\n",
		"defaults: &base\n",
		"copy: *base\n",
		"name: !!str demo\n",
	}
	for _, input := range cases {
		if _, err := Parse(input); err == nil {
			t.Fatalf("Parse returned nil error for %q", input)
		}
	}
}
