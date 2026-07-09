package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// stubFontFetcher swaps in a fake font fetcher for the duration of a test so
// the suite never touches the network.
func stubFontFetcher(t *testing.T, fn func(family string) ([]byte, error)) {
	t.Helper()
	prev := fontFetcher
	fontFetcher = fn
	t.Cleanup(func() { fontFetcher = prev })
}

// ---- Item 1: self-hosted fonts ------------------------------------------

// A theme naming fonts must ship the matching woff2 files, and every file the
// emitted @font-face references must actually exist in the file set — no
// silent /fonts/*.woff2 404.
func TestFontsShippedMatchCSS(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		return []byte("wOF2-" + family), nil
	})
	bp := Blueprint{
		App: BlueprintApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans", "font_body": "Demo Serif"},
		},
	}
	files := filesByName(mustRenderBlueprintFiles(t, bp))
	for _, want := range []string{"static/fonts/test-sans.woff2", "static/fonts/demo-serif.woff2"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("expected font file %s to be shipped; got files: %v", want, keysOf(files))
		}
	}
	css := files["app.go"]
	for _, ref := range []string{"/fonts/test-sans.woff2", "/fonts/demo-serif.woff2"} {
		if !strings.Contains(css, ref) {
			t.Fatalf("fontFaceCSS should reference %s:\n%s", ref, css)
		}
	}
	// Every referenced woff2 must be shipped (the whole point of the fix).
	if len(blueprintMissingFontSlugs(bp, mustRenderBlueprintFiles(t, bp))) != 0 {
		t.Fatalf("no font file should be missing when the fetch succeeds")
	}
}

// When the generate-time fetch fails (offline), the app is still emitted but
// the missing files are reported so generate can warn — no silent fallback.
func TestFontsOfflineReportedMissing(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		return nil, errStubOffline
	})
	bp := Blueprint{
		App: BlueprintApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans"},
		},
	}
	fontFiles, missing := blueprintFontAssets(bp)
	if len(fontFiles) != 0 {
		t.Fatalf("offline fetch must ship no font files, got %d", len(fontFiles))
	}
	if len(missing) != 1 || missing[0] != "static/fonts/test-sans.woff2" {
		t.Fatalf("expected missing static/fonts/test-sans.woff2, got %v", missing)
	}
	// The app still renders (no font file present) and reports the gap.
	files := mustRenderBlueprintFiles(t, bp)
	if got := blueprintMissingFontSlugs(bp, files); len(got) != 1 {
		t.Fatalf("expected 1 missing font, got %v", got)
	}
}

// The generated main.go boot-checks the self-hosted font files so a missing
// asset warns at startup rather than 404ing silently.
func TestFontsBootCheckEmitted(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) { return []byte("x"), nil })
	bp := Blueprint{
		App: BlueprintApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans"},
		},
	}
	main := renderBlueprintMain(bp)
	if !strings.Contains(main, "static/fonts/test-sans.woff2") || !strings.Contains(main, "os.Stat(fontFile)") {
		t.Fatalf("main.go should boot-check the font file:\n%s", main)
	}
}

var errStubOffline = &stubErr{"offline"}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }

// ---- Item 2: chart/stat source registration ------------------------------

// A chart sourced from an entity that has NO list/detail screen must still get
// a ResourceConfig, or statValue/groupCounts render a silent "—".
func TestChartSourceRegistersResource(t *testing.T) {
	crudOn := true
	bp := Blueprint{
		App: BlueprintApp{Name: "Dash", Module: "example.com/dash"},
		Entities: []framework.EntityDeclaration{{
			Name: "tickets",
			CRUD: &crudOn,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string"},
				{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
			},
		}},
		Screens: []BlueprintScreen{{
			Name:  "dashboard",
			Route: "/",
			Body: []BlueprintBlock{{
				Kind: "bar_chart",
				Props: map[string]any{
					"title":  "By status",
					"source": map[string]any{"entity": "tickets", "group_by": "status"},
				},
			}},
		}},
	}
	files := filesByName(mustRenderBlueprintFiles(t, bp))
	// tickets has no list/detail screen, so its appResources wiring lands in a
	// resource-only screen_tickets_crud.go (the dashboard sources it via a chart).
	crud := files["screen_tickets_crud.go"]
	if crud == "" {
		t.Fatalf("missing screen_tickets_crud.go; files=%v", sortedFileNames(mustRenderBlueprintFiles(t, bp)))
	}
	if !strings.Contains(crud, `appResources["tickets"] = ResourceConfig{`) {
		t.Fatalf("tickets must be registered in appResources even without a list screen:\n%s", crud)
	}
}

// A chart/stat source pointing at a crud-disabled entity is rejected up front
// (otherwise MustCrudHandler would panic at boot).
func TestChartSourceCrudDisabledRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "crud") {
		t.Fatalf("expected a crud-enabled validation error, got %v", err)
	}
}

// ---- Item 3: seed enum distribution --------------------------------------

// The default (unweighted) distribution of a count-seed must NOT be a uniform
// N/K split — that's the "8/8/8/8" round-robin the fix removes.
func TestSeedDefaultDistributionNotUniform(t *testing.T) {
	values := []string{"open", "in_progress", "resolved", "closed"}
	seq := blueprintEnumDistribution("ticket", "status", values, nil, 30)
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
	seq2 := blueprintEnumDistribution("ticket", "status", values, nil, 30)
	if strings.Join(seq, ",") != strings.Join(seq2, ",") {
		t.Fatalf("distribution is not deterministic")
	}
}

// An explicit weights map is respected (roughly proportionally).
func TestSeedWeightsRespected(t *testing.T) {
	values := []string{"open", "in_progress", "resolved", "urgent"}
	weights := map[string]int{"open": 5, "in_progress": 3, "resolved": 8, "urgent": 1}
	seq := blueprintEnumDistribution("ticket", "status", values, weights, 34)
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
	decl := framework.EntityDeclaration{
		Name: "tickets",
		CRUD: &crudOn,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "status", Type: "enum", Values: []string{"open", "in_progress", "resolved", "closed"}},
		},
	}
	rows := blueprintGenerateSeedRows(decl, 20, nil)
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
	writeTestFile(t, path, `
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
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if len(bp.Seed) != 1 || bp.Seed[0].Count != 12 {
		t.Fatalf("count not decoded: %+v", bp.Seed)
	}
	if bp.Seed[0].Weights["status"]["open"] != 5 {
		t.Fatalf("weights not decoded: %+v", bp.Seed[0].Weights)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
