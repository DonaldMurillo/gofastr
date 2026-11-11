package appgen

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func loadH1Spec(t *testing.T, screens string) []GeneratedFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, `app:
  name: Desk
  module: example.com/desk
entities:
  - name: tickets
    crud: true
    fields:
      - name: subject
        type: string
screens:
`+screens)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A dashboard of stat cards draws no <h1> (a screen title: is the tab
// title, not a heading), so the generated app's own axe gate failed
// page-has-heading-one. A screen nothing draws an <h1> on opens with a
// page header of its title; one that draws its own keeps it alone.
func TestScreenWithNoH1GetsPageHeader(t *testing.T) {
	const header = "ui.PageHeader(ui.PageHeaderConfig{Title: "
	for _, tc := range []struct {
		name, body string
		headers    int
	}{
		{"stats only", `      - kind: stat_row
        children:
          - kind: stat_card
            props:
              label: Open
              value: "12"
`, 1},
		{"own page header", `      - kind: page_header
        props:
          title: Metrics
      - kind: stat_card
        props:
          label: Open
          value: "12"
`, 1},
		{"level 1 heading", `      - type: heading
        text: Metrics
`, 0},
		// A node heading's level is a prop; it is not an <h1>.
		{"node heading level 3", `      - kind: heading
        props:
          level: 3
          text: Metrics
`, 1},
		{"markdown h1", `      - kind: markdown
        text: "# Metrics\n\nBody."
`, 0},
		// type: hN renders at level: when one is set, so an h1 set to
		// level 3 is an <h3> and an h2 set to level 1 is an <h1>.
		{"h1 at level 3", `      - type: h1
        level: 3
        text: Metrics
`, 1},
		{"h2 at level 1", `      - type: h2
        level: 1
        text: Metrics
`, 0},
		// core/markdown reads "# " only at the start of a line and has
		// no setext headings: none of these draws an <h1>.
		{"markdown tab-indented", `      - kind: markdown
        text: "Example:\n\n\t# not a heading\n"
`, 1},
		{"markdown space-indented", `      - kind: markdown
        text: "   # not a heading\n"
`, 1},
		{"markdown setext", `      - kind: markdown
        text: "Metrics\n=======\n"
`, 1},
		{"markdown fenced", "      - kind: markdown\n        text: \"```\\n# code\\n```\\n\"\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := loadH1Spec(t, "  - name: metrics\n    route: /metrics\n    title: Key Metrics\n    body:\n"+tc.body)
			src := fileContent(files, "screen_metrics.go")
			if got := strings.Count(src, header); got != tc.headers {
				t.Fatalf("%d page headers, want %d:\n%s", got, tc.headers, src)
			}
			if tc.headers == 1 && tc.name != "own page header" && !strings.Contains(src, `"Key Metrics"`) {
				t.Fatalf("page header does not say the screen title:\n%s", src)
			}
		})
	}
}

// An entity list's title drops to <h2> under an earlier <h1>. A node
// heading set to level 3 is not one, so the list keeps its <h1>.
func TestListAfterH3KeepsH1(t *testing.T) {
	files := loadH1Spec(t, `  - name: tickets
    route: /tickets
    body:
      - kind: heading
        props:
          level: 3
          text: Queue
      - kind: entity_list
        entity: tickets
        text: Tickets
        fields: [subject]
`)
	src := fileContent(files, "screen_tickets_crud.go")
	if !regexp.MustCompile(`\.Heading\([^,]+, 1\)`).MatchString(src) {
		t.Fatalf("list title is not level 1:\n%s", src)
	}
}
