package actionscan

import (
	"encoding/json"
	"os"
	"testing"
)

// The corpus both embed gates run (framework/uihost
// TestBootScanAgreesWithCorpus, cmd/check-embed/embedcheck
// TestAnalyzerScanAgreesWithCorpus).
func TestServerActionCallCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.json")
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
	for _, c := range cases {
		if got := ServerActionCall(c.JS); got != c.Want {
			t.Errorf("ServerActionCall(%q) = %v, want %v (%s)", c.JS, got, c.Want, c.Why)
		}
	}
}

// An unterminated regex, string, comment or template ends the scan
// without a call and without running off the end.
func TestServerActionCallUnterminated(t *testing.T) {
	for _, src := range []string{"x = /abc", "'abc", "/* abc", "`abc ${", "`${ G.toast(", "a /", "/[abc/"} {
		if ServerActionCall(src) {
			t.Errorf("ServerActionCall(%q) = true", src)
		}
	}
	// A "/" that cannot be a regex (a line break before its end) is read
	// as code again, so the call after it is still seen.
	if !ServerActionCall("x = a\n/ b; G.serverAction('s')") {
		t.Error("a division across a line break hid the call after it")
	}
}
