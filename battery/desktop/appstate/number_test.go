package appstate

import (
	"fmt"
	"path/filepath"
	"testing"
)

// Get decoded with json.Unmarshal, so a stored map read back with
// 9007199254740992 where Set wrote 9007199254740993.
func TestGetKeepsBigIntegers(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.ids", map[string]any{"n": int64(9007199254740993)}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Get[map[string]any](s, "page.ids")
	if !ok || err != nil {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if n := fmt.Sprint(got["n"]); n != "9007199254740993" {
		t.Errorf("n = %s, want 9007199254740993", n)
	}
}
