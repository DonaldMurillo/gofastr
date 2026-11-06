package verifycmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBaselineJSONKeepsNotices(t *testing.T) {
	directory := unguardedModule(t)
	output, code := captureVerify(t, []string{"--root", directory, "--no-vet", "--json", "--baseline-write", "--changed"})
	var document struct {
		Notices []string `json:"notices"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil || code != 0 {
		t.Fatalf("exit=%d, error=%v, stdout=%s", code, err, output)
	}
	if !strings.Contains(strings.Join(document.Notices, "\n"), "not a git repository") {
		t.Fatalf("baseline document lost notices: %s", output)
	}
}
