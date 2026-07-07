package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRuntimeContractCoversArchitectureAttrs pins the sync contract
// between core-ui/ARCHITECTURE.md (the repo's source of truth for the
// UI/runtime contract) and the embedded extract content/runtime-contract.md
// (what `gofastr docs` readers see). Every data-fui-* attribute that
// leads a row of the ARCHITECTURE attribute table must appear in the
// embedded doc — so adding an attribute upstream without carrying it
// into the extract fails here, in the same spirit as
// core-ui/runtime/doc_manifest_test.go.
func TestRuntimeContractCoversArchitectureAttrs(t *testing.T) {
	arch, err := os.ReadFile(filepath.Join("..", "..", "core-ui", "ARCHITECTURE.md"))
	if err != nil {
		// Outside the repo checkout (embedded-only consumers) there is
		// nothing to sync against.
		t.Skipf("core-ui/ARCHITECTURE.md not readable: %v", err)
	}
	embedded, err := Get("runtime-contract")
	if err != nil {
		t.Fatalf("embedded runtime-contract doc missing: %v", err)
	}
	attrRe := regexp.MustCompile(`data-fui-[a-z0-9-]+`)
	var missing []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(string(arch), "\n") {
		// Attribute-table rows start with a backticked data-fui-* name
		// in the first cell.
		if !strings.HasPrefix(ln, "| `data-fui-") {
			continue
		}
		cells := strings.SplitN(ln, "|", 3)
		if len(cells) < 3 {
			continue
		}
		for _, attr := range attrRe.FindAllString(cells[1], -1) {
			if seen[attr] {
				continue
			}
			seen[attr] = true
			if !strings.Contains(string(embedded), attr) {
				missing = append(missing, attr)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no data-fui-* table rows found in core-ui/ARCHITECTURE.md — did the attribute table move? Update this parser and content/runtime-contract.md together")
	}
	if len(missing) > 0 {
		t.Errorf("content/runtime-contract.md is out of sync with core-ui/ARCHITECTURE.md — missing attributes: %s\n(the extract must be updated in the same commit as the ARCHITECTURE table; see the SYNC NOTE in runtime-contract.md)", strings.Join(missing, ", "))
	}
}
