package appspec

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestExampleAppSpecsLoad validates every examples/<name>/gofastr.yml parses
// and validates cleanly. This is the cheap half of the gate: it runs under
// -short and catches an app spec that no longer decodes.
//
// It is NOT sufficient on its own. Parsing proves nothing about the Go the
// generator emits from the parse. See TestExampleAppSpecsBoot (cmd/gofastr).
//
//gofastr:test-reads examples/*/gofastr.yml
func TestExampleAppSpecsLoad(t *testing.T) {
	for _, path := range clitest.ExampleAppSpecs(t) {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			if _, err := LoadAppSpec(path); err != nil {
				t.Errorf("%s failed to load: %v", path, err)
			}
		})
	}
}
