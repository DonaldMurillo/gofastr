package embedcheck

//gofastr:test-reads internal/actionscan/testdata/

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// check-embed and the boot walk read ClientJS the same way: both run the
// shared corpus in internal/actionscan/testdata/cases.json. The boot
// walk's twin is framework/uihost TestBootScanAgreesWithCorpus.
func TestAnalyzerScanAgreesWithCorpus(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "..", "internal", "actionscan", "testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		JS   string `json:"js"`
		Want bool   `json:"want"`
		Why  string `json:"why"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("setup broken: %d corpus cases", len(cases))
	}
	for _, c := range cases {
		if got := executableServerActionCall(c.JS); got != c.Want {
			t.Errorf("check-embed executableServerActionCall(%q) = %v, want %v (%s)", c.JS, got, c.Want, c.Why)
		}
	}
}
