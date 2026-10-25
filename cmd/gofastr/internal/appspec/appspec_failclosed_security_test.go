package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The table above exercises decodeAppSpecString (the shared decode entry
// for validate AND generate). This pins the same failure through the full
// loadAppSpec path (decode + validate) and checks the error names the key,
// so a reader of the failure knows which line to fix.
func TestFlatProtectiveBoolErrorsFromLoad(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{"multi_tenant", "yes"},
		{"soft_delete", "yes"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		clitest.WriteTestFile(t, path, "app:\n  name: app\n  module: local/app\nentities:\n  - name: tickets\n    fields:\n      - name: title\n        type: string\n    "+tc.key+": "+tc.value+"\n")
		_, err := LoadAppSpec(path)
		if err == nil {
			t.Fatalf("%s: %s: expected a load error naming the key", tc.key, tc.value)
		}
		if !strings.Contains(err.Error(), tc.key) {
			t.Fatalf("%s: error should name the key, got: %v", tc.key, err)
		}
	}
}
