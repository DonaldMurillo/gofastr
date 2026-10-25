package newcmd

import (
	"testing"
)

func TestValidateScaffoldNameDotDot(t *testing.T) {
	// Contains ".." but no path separator → hits the dedicated branch.
	if err := validateScaffoldName("a..b"); err == nil {
		t.Fatal("expected error for embedded '..'")
	}
}
