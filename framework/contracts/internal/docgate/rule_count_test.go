package docgate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	// The analyzers package registers the catalog's rules.
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// TestAdvertisedRuleCountMatchesTheCatalog pins the numbers the docs
// quote. A count in prose drifts the moment a rule is added, this file
// shipped "43 rules" while the catalog held 46, and a README that
// undersells the tool by three rules is a small lie that compounds.
//
// The fix when this fails is one number per document, not deleting the
// test: the count is worth stating, and stating it is worth checking.
// CHANGELOG.md is not checked: a release entry records the count at that
// release, and the entry moves to changelog/ ten minors later.
//
//gofastr:test-reads /README.md
func TestAdvertisedRuleCountMatchesTheCatalog(t *testing.T) {
	n := len(contracts.AllRules())
	want := fmt.Sprintf("%d rules", n)
	wantAlt := fmt.Sprintf("%d contract rules", n)

	for _, doc := range []string{"README.md"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		text := string(body)
		if !strings.Contains(text, want) && !strings.Contains(text, wantAlt) {
			t.Errorf("%s does not state the current rule count (%q)", doc, want)
		}
		// A stale count left behind next to the new one is the same bug.
		for stale := 30; stale < 200; stale++ {
			if stale == n {
				continue
			}
			for _, phrase := range []string{
				fmt.Sprintf("%d rules across", stale),
				fmt.Sprintf("runs %d contract rules", stale),
				fmt.Sprintf("the %d rules that", stale),
			} {
				if strings.Contains(text, phrase) {
					t.Errorf("%s still says %q; the catalog has %d", doc, phrase, n)
				}
			}
		}
	}
}
