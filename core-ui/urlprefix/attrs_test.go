package urlprefix

//gofastr:test-reads core-ui/ARCHITECTURE.md

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// notRewritten names the URL-valued attributes the rewriter leaves
// alone on purpose, each with why.
var notRewritten = map[string]string{
	// An asset (the linked stylesheet a page would name), shared by
	// every locale like every other asset.
	"data-cui-bundle-href": "asset",
}

// TestURLAttrsAllCovered keeps URLAttrs in step with the attribute
// table: an attribute documented as taking a <path> or <url> is either
// rewritten or named in notRewritten.
func TestURLAttrsAllCovered(t *testing.T) {
	doc, err := os.ReadFile("../ARCHITECTURE.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `(data-cui-[a-z-]+)=\"<(?:path|url)>\"`").FindAllSubmatch(doc, -1)
	if len(rows) < 5 {
		t.Fatalf("found %d URL-valued rows: the table format changed", len(rows))
	}
	for _, m := range rows {
		name := string(m[1])
		if _, skip := notRewritten[name]; skip {
			continue
		}
		if !slices.Contains(URLAttrs, name) {
			t.Errorf("%s takes a URL but urlprefix.URLAttrs does not rewrite it", name)
		}
	}
}
