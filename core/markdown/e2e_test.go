package markdown

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden lets us regenerate the .html fixtures after intentional output
// changes: `go test ./core/markdown -update`. CI runs without the flag and
// compares against the checked-in expected output.
var updateGolden = flag.Bool("update", false, "rewrite golden HTML fixtures")

// TestKitchenSinkGolden renders a kitchen-sink Markdown document that
// exercises every supported block and inline element, and compares the
// full HTML output against a checked-in fixture. This is the end-to-end
// test the unit tests are not.
func TestKitchenSinkGolden(t *testing.T) {
	mdPath := filepath.Join("testdata", "kitchen-sink.md")
	htmlPath := filepath.Join("testdata", "kitchen-sink.html")
	jsonPath := filepath.Join("testdata", "kitchen-sink.frontmatter")

	src, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	doc := Render(string(src))

	if *updateGolden {
		if err := os.WriteFile(htmlPath, []byte(doc.HTML), 0o644); err != nil {
			t.Fatalf("write golden html: %v", err)
		}
		fm := serializeFrontmatter(doc.Frontmatter, doc.Title)
		if err := os.WriteFile(jsonPath, []byte(fm), 0o644); err != nil {
			t.Fatalf("write golden frontmatter: %v", err)
		}
		t.Logf("updated %s and %s", htmlPath, jsonPath)
		return
	}

	wantHTML, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read golden html (run with -update to create): %v", err)
	}
	if string(doc.HTML) != string(wantHTML) {
		t.Errorf("HTML mismatch (run with -update to refresh).\n--- got ---\n%s\n--- want ---\n%s",
			doc.HTML, wantHTML)
	}

	wantFM, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read golden frontmatter: %v", err)
	}
	gotFM := serializeFrontmatter(doc.Frontmatter, doc.Title)
	if gotFM != string(wantFM) {
		t.Errorf("frontmatter mismatch.\n--- got ---\n%s\n--- want ---\n%s", gotFM, wantFM)
	}
}

// TestRendersProjectDocs exercises every Markdown file under ../../docs as a
// regression check: each one must render without panicking, must produce
// non-empty HTML, and must not leak a raw <script> or </h2 tag (a smoke test
// that escaping is in effect on real-world inputs).
func TestRendersProjectDocs(t *testing.T) {
	docsDir := filepath.Join("..", "..", "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Skipf("docs dir not available: %v", err)
		return
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := entry.Name()
		path := filepath.Join(docsDir, name)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			doc := Render(string(src))
			html := string(doc.HTML)
			if html == "" {
				t.Errorf("%s rendered empty", name)
			}
			if strings.Contains(html, "<script>") || strings.Contains(html, "<iframe") {
				t.Errorf("%s leaked raw HTML: %s", name, html)
			}
			// Every doc should produce at least one heading or paragraph.
			if !strings.Contains(html, "<h") && !strings.Contains(html, "<p>") {
				t.Errorf("%s produced no structural element:\n%s", name, html)
			}
		})
		checked++
	}
	if checked == 0 {
		t.Fatal("no markdown files were rendered")
	}
	t.Logf("rendered %d project docs", checked)
}

func serializeFrontmatter(fm map[string]string, title string) string {
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	// stable order for deterministic golden file
	sortStrings(keys)
	var sb strings.Builder
	sb.WriteString("Title: " + title + "\n")
	for _, k := range keys {
		sb.WriteString(k + " = " + fm[k] + "\n")
	}
	return sb.String()
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
