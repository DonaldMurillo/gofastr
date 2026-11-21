package appspec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/app-spec.md /framework/docs/content/entity-declarations.md
func TestRewrittenAppSpecDocExamplesLoad(t *testing.T) {
	const screen = `screens:
  - name: example
    route: /
    title: Example
    body:
`
	const orders = `entities:
  - name: orders
    primary_key: [store, number]
    fields:
      - name: store
        type: string
      - name: number
        type: int
`
	const customers = `entities:
  - name: customers
    fields:
      - name: status
        type: string
      - name: mrr
        type: decimal
`
	yamlFence := regexp.MustCompile("(?m)^\x60\x60\x60yaml\n([\\s\\S]*?)^\x60\x60\x60$")
	for _, tc := range []struct {
		doc, needle, prefix, indent string
	}{
		{"app-spec", "- type: link\n  href: /docs", screen, "      "},
		{"app-spec", "title_fields: [number]", "", ""},
		{"app-spec", "primary_key: [student_id, course_id]", "", ""},
		{"app-spec", "foreign_key: [order_store, order_no]", orders, ""},
		{"app-spec", "name: Starter", screen, "      "},
		{"app-spec", "label: Monthly revenue", customers + screen, "      "},
		{"entity-declarations", "cross_owner_read: tickets:read:all", "", ""},
		{"entity-declarations", "search_fields: [title, body]", "", ""},
	} {
		t.Run(tc.doc+"/"+tc.needle, func(t *testing.T) {
			doc, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "framework", "docs", "content", tc.doc+".md"))
			if err != nil {
				t.Fatal(err)
			}
			var snippets []string
			for _, block := range yamlFence.FindAllStringSubmatch(string(doc), -1) {
				if strings.Contains(block[1], tc.needle) {
					snippets = append(snippets, block[1])
				}
			}
			if len(snippets) != 1 {
				t.Fatalf("found %d YAML examples containing %q, want one", len(snippets), tc.needle)
			}
			snippet := snippets[0]
			if tc.indent != "" {
				snippet = tc.indent + strings.ReplaceAll(strings.TrimSuffix(snippet, "\n"), "\n", "\n"+tc.indent) + "\n"
			}
			path := filepath.Join(t.TempDir(), "gofastr.yml")
			if err := os.WriteFile(path, []byte(tc.prefix+snippet), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAppSpec(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
