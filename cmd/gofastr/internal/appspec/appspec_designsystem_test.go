package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A chart without a source used to compile into an HTML comment. Now it's
// a validation error naming the fix.
func TestChartWithoutSourceRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Charts
  module: example.com/charts
entities:
  - name: orders
    fields:
      - name: status
        type: string
screens:
  - name: dashboard
    route: /
    body:
      - kind: line_chart
        props:
          title: Broken
`)
	if _, err := LoadAppSpec(path); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("chart without source should fail validation mentioning source, got: %v", err)
	}
}
