package gallery

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestCatalogCountMatchesDocs pins len(Catalog) at the count advertised in
// the gallery package doc, agents.md, and the CHANGELOG release note. Add a
// component → bump this constant and the three doc sites in the same commit.
func TestCatalogCountMatchesDocs(t *testing.T) {
	const advertised = 141
	if got := len(Catalog); got != advertised {
		t.Errorf("len(gallery.Catalog) = %d, want %d — the catalog drifted "+
			"from the count in the package doc, agents.md, and CHANGELOG; "+
			"bump the constant and update all three doc sites", got, advertised)
	}
}

// TestSiteComponentsCommentMatchesCatalog pins the "the N entries" count in
// examples/site/components.go's header comment to len(Catalog). That file is
// where the catalog was extracted FROM, so its comment is a doc site too,
// and it stayed at 139 while every other site moved to 141. Parsing the
// number (rather than hard-coding 141) keeps the gate honest in both
// directions: if the catalog grows, the comment must grow with it, and a
// stale count fails here regardless of which way it drifted.
func TestSiteComponentsCommentMatchesCatalog(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "site", "components.go"))
	if err != nil {
		t.Fatalf("read examples/site/components.go: %v", err)
	}
	m := regexp.MustCompile(`the (\d+) entries`).FindSubmatch(b)
	if m == nil {
		t.Skip(`components.go no longer carries a "the N entries" header comment`)
	}
	comment, _ := strconv.Atoi(string(m[1]))
	if comment != len(Catalog) {
		t.Errorf("examples/site/components.go header says \"the %d entries\" but "+
			"len(Catalog) = %d; the comment drifted from the catalog it was "+
			"extracted from", comment, len(Catalog))
	}
}
