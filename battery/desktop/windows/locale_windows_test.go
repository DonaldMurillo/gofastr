//go:build windows && amd64

package windows

import (
	"reflect"
	"syscall"
	"testing"
)

func TestSplitMultiSZ(t *testing.T) {
	var buf []uint16
	for _, s := range []string{"fr-CA", "en-US"} {
		u, _ := syscall.UTF16FromString(s)
		buf = append(buf, u...)
	}
	buf = append(buf, 0)
	if got := splitMultiSZ(buf); !reflect.DeepEqual(got, []string{"fr-CA", "en-US"}) {
		t.Fatalf("splitMultiSZ = %q", got)
	}
	if got := splitMultiSZ([]uint16{0, 0}); got != nil {
		t.Fatalf("empty list = %q", got)
	}
}

// The live call answers at least one display language on any Windows
// install.
func TestPreferredLanguagesLive(t *testing.T) {
	if got := preferredLanguages(); len(got) == 0 || got[0] == "" {
		t.Fatalf("preferredLanguages = %q", got)
	}
}
