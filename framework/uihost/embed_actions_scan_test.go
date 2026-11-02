package uihost

//gofastr:test-reads internal/actionscan/testdata/

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The boot walk (serverActionCall) and `gofastr build`'s check-embed
// analyzer must answer the same question the same way: does this
// ClientJS call G.serverAction? The boot walk matched the bytes
// "G.serverAction(" anywhere, so a mention in a comment, a string or
// template text passed check-embed (which reads code) and then panicked
// at Mount. Both now delegate to internal/actionscan, and both run the
// same corpus (internal/actionscan/testdata/cases.json); check-embed's
// twin is TestAnalyzerScanAgreesWithCorpus.
func TestBootScanAgreesWithCorpus(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "internal", "actionscan", "testdata", "cases.json"))
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
		if got := serverActionCall(c.JS); got != c.Want {
			t.Errorf("boot walk serverActionCall(%q) = %v, want %v (%s)", c.JS, got, c.Want, c.Why)
		}
	}
}
