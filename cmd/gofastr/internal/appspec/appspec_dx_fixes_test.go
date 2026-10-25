package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A chart/stat source pointing at a crud-disabled entity is rejected up front
// (otherwise MustCrudHandler would panic at boot).
func TestChartSourceCrudDisabledRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Dash
  module: example.com/dash
entities:
  - name: tickets
    crud: false
    fields:
      - name: status
        type: enum
        values: [open, closed]
screens:
  - name: dashboard
    route: /
    body:
      - kind: bar_chart
        props:
          title: By status
          source:
            entity: tickets
            group_by: status
`)
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), "crud") {
		t.Fatalf("expected a crud-enabled validation error, got %v", err)
	}
}

// ---- Item 3: seed enum distribution --------------------------------------

// The default (unweighted) distribution of a count-seed must NOT be a uniform
// N/K split, the "8/8/8/8" round-robin the fix removes.
func TestSeedDefaultDistributionNotUniform(t *testing.T) {
	values := []string{"open", "in_progress", "resolved", "closed"}
	seq := appSpecEnumDistribution("ticket", "status", values, nil, 30)
	if len(seq) != 30 {
		t.Fatalf("expected 30 values, got %d", len(seq))
	}
	counts := map[string]int{}
	for _, v := range seq {
		counts[v]++
	}
	// A uniform split of 30 over 4 is 8/8/7/7 (spread 1). Require a real skew.
	min, max := 30, 0
	for _, v := range values {
		if counts[v] < min {
			min = counts[v]
		}
		if counts[v] > max {
			max = counts[v]
		}
	}
	if max-min < 3 {
		t.Fatalf("default distribution too uniform (spread %d): %v", max-min, counts)
	}
	// Determinism: same inputs → same sequence.
	seq2 := appSpecEnumDistribution("ticket", "status", values, nil, 30)
	if strings.Join(seq, ",") != strings.Join(seq2, ",") {
		t.Fatalf("distribution is not deterministic")
	}
}

// An explicit weights map is respected (roughly proportionally).
func TestSeedWeightsRespected(t *testing.T) {
	values := []string{"open", "in_progress", "resolved", "urgent"}
	weights := map[string]int{"open": 5, "in_progress": 3, "resolved": 8, "urgent": 1}
	seq := appSpecEnumDistribution("ticket", "status", values, weights, 34)
	counts := map[string]int{}
	for _, v := range seq {
		counts[v]++
	}
	// total weight 17 over 34 rows ⇒ 2 rows per weight-unit.
	if counts["resolved"] <= counts["open"] || counts["open"] <= counts["in_progress"] || counts["in_progress"] <= counts["urgent"] {
		t.Fatalf("weights not respected: %v", counts)
	}
	if counts["urgent"] < 1 || counts["urgent"] > 4 {
		t.Fatalf("urgent (weight 1) count off: %v", counts)
	}
}

// count: N generates that many rows with the enum column varied.
func TestSeedCountGeneratesVariedRows(t *testing.T) {
	crudOn := true
	decl := framework.EntityDeclaration{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "tickets", Exposure: &framework.ExposureDeclaration{CRUD: &crudOn}, Fields: []framework.FieldDeclaration{
		{Name: "title", Type: "string", Required: true},
		{Name: "status", Type: "enum", Values: []string{"open", "in_progress", "resolved", "closed"}},
	},
	}
	rows := AppSpecGenerateSeedRows(decl, 20, nil)
	if len(rows) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(rows))
	}
	distinct := map[string]bool{}
	for _, r := range rows {
		if r["title"] == nil || r["title"] == "" {
			t.Fatalf("required title must be filled: %v", r)
		}
		distinct[r["status"].(string)] = true
	}
	if len(distinct) < 3 {
		t.Fatalf("expected varied statuses, got %v", distinct)
	}
}

// count + weights parse cleanly from YAML.
func TestSeedCountWeightsDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Dash
  module: example.com/dash
entities:
  - name: tickets
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [open, closed]
seed:
  - entity: tickets
    count: 12
    weights:
      status:
        open: 5
        closed: 1
`)
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if len(bp.Seed) != 1 || bp.Seed[0].Count != 12 {
		t.Fatalf("count not decoded: %+v", bp.Seed)
	}
	if bp.Seed[0].Weights["status"]["open"] != 5 {
		t.Fatalf("weights not decoded: %+v", bp.Seed[0].Weights)
	}
}
