package packcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func chartAppSpecYAML(chartKind string) string {
	return `
app:
  name: Charts
  module: example.com/charts
entities:
  - name: orders
    fields:
      - name: status
        type: string
screens:
  - name: dashboard
    route: /
    body:
      - kind: ` + chartKind + `
        props:
          title: Orders by status
          source:
            entity: orders
            group_by: status
`
}

// The generator must not emit bespoke CSS classes (.mrd-*, .gofastr-*). The
// exact regression CLAUDE.md documents as fixed 2026-06. Charts with
// titles compose ui.Card; muted placeholders compose ui.EmptyValue.
func TestGeneratorEmitsNoBespokeClasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, chartAppSpecYAML("bar_chart"))
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	for _, f := range mustRenderAppSpecFiles(t, bp) {
		for _, banned := range []string{"mrd-", `class: "gofastr-`, `class=\"gofastr-`} {
			if strings.Contains(f.Content, banned) {
				t.Errorf("%s: generated output contains bespoke class marker %q", f.Name, banned)
			}
		}
	}
	screens := allScreenContent(mustRenderAppSpecFiles(t, bp))
	if !strings.Contains(screens, `ui.Card(ui.CardConfig{Heading: "Orders by status", HeadingLevel: 2}`) {
		t.Errorf("titled chart should compose ui.Card, got:\n%s", screens)
	}
}

// line_chart must render a real chart. It used to pass validation and
// then silently disappear into a noderender comment.
func TestLineChartRenders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, chartAppSpecYAML("line_chart"))
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	screens := allScreenContent(mustRenderAppSpecFiles(t, bp))
	if !strings.Contains(screens, `appUI.LineChart(ctx, "orders", "status")`) {
		t.Fatalf("line_chart should render via appUI, got:\n%s", screens)
	}
	if strings.Contains(screens, "noderender") {
		t.Fatalf("line_chart fell through to the node renderer:\n%s", screens)
	}
}
