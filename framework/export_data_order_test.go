package framework

import (
	"os"
	"path/filepath"
	"testing"
)

// A restored row with two refusable columns names the same one on every
// read: readNDJSON ranged the decoded row, so the refusal named whichever
// column the iteration reached first.
func TestReadNDJSONRefusalIsDeterministic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.ndjson")
	if err := os.WriteFile(path, []byte(`{"a":[1],"b":[2],"c":[3],"d":[4]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, first := readNDJSON(path)
	if first == nil {
		t.Fatal("array columns accepted")
	}
	for range 64 {
		if _, err := readNDJSON(path); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between reads: %q then %v", first, err)
		}
	}
}
