package uihost

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /core-ui/ARCHITECTURE.md /framework/docs/content/runtime-contract.md
func TestEnvelopeFillDocsHaveNoHash(t *testing.T) {
	for _, path := range []string{"../../core-ui/ARCHITECTURE.md", "../docs/content/runtime-contract.md"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, row, found := strings.Cut(string(source), "| `X-Gofastr-Envelope: 2` |")
		row, _, _ = strings.Cut(row, "\n")
		if !found || strings.Contains(row, "hashed template") || !strings.Contains(row, "one template per kept-layer fill") {
			t.Errorf("%s: fills envelope must describe unhashed templates: %s", path, row)
		}
	}
}
