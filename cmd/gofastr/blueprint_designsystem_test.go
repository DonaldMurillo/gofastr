package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func chartBlueprintYAML(chartKind string) string {
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

// The generator must not emit bespoke CSS classes (.mrd-*, .gofastr-*) —
// the exact regression CLAUDE.md documents as fixed 2026-06. Charts with
// titles compose ui.Card; muted placeholders compose ui.EmptyValue.
func TestGeneratorEmitsNoBespokeClasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, chartBlueprintYAML("bar_chart"))
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		for _, banned := range []string{"mrd-", `class: "gofastr-`, `class=\"gofastr-`} {
			if strings.Contains(f.content, banned) {
				t.Errorf("%s: generated output contains bespoke class marker %q", f.name, banned)
			}
		}
	}
	byName := filesByName(mustRenderBlueprintFiles(t, bp))
	screens := byName[filepath.Join("blueprint", "screens.go")]
	if !strings.Contains(screens, `ui.Card(ui.CardConfig{Heading: "Orders by status"}`) {
		t.Errorf("titled chart should compose ui.Card, got:\n%s", screens)
	}
}

// line_chart must render a real chart — it used to pass validation and
// then silently disappear into a noderender comment.
func TestLineChartRenders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, chartBlueprintYAML("line_chart"))
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	byName := filesByName(mustRenderBlueprintFiles(t, bp))
	screens := byName[filepath.Join("blueprint", "screens.go")]
	if !strings.Contains(screens, "blueprintLineChart(ctx, \"orders\", \"status\")") {
		t.Fatalf("line_chart should render via blueprintLineChart, got:\n%s", screens)
	}
	if strings.Contains(screens, "noderender") {
		t.Fatalf("line_chart fell through to the node renderer:\n%s", screens)
	}
}

// A chart without a source used to compile into an HTML comment. Now it's
// a validation error naming the fix.
func TestChartWithoutSourceRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
      - kind: line_chart
        props:
          title: Broken
`)
	if _, err := loadBlueprint(path); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("chart without source should fail validation mentioning source, got: %v", err)
	}
}
