package gallery

import "testing"

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
