package cli

import (
	"testing"
)

// Edit distance now lives in core/fuzzy (shared with framework/filter) and is
// tested there; here we only cover the CLI-local variadic min helper.
func TestMin(t *testing.T) {
	if Min(3, 1, 2) != 1 {
		t.Fatalf("min broken")
	}
	if Min(5) != 5 {
		t.Fatalf("min single-arg broken")
	}
}
