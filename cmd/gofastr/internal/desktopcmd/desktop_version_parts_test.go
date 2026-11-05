package desktopcmd

import "testing"

func TestWindowsVersionsUseCoreNumbers(t *testing.T) {
	for _, version := range []string{"1.2.3-rc.1.2", "1.2.3-rc.1", "1.2.3+build.123.456", "1.2.3-rc.1+build.42"} {
		parts, err := windowsVersionParts(version)
		if err != nil || parts != [4]uint16{1, 2, 3, 0} {
			t.Errorf("windowsVersionParts(%q) = %v, %v; want [1 2 3 0]", version, parts, err)
		}
	}
	for _, version := range []string{"1.2.3-", "1.2.3+", "1.2.3-01", "1.65536.3-rc.1", "1.2.3.4.5"} {
		if _, err := windowsVersionParts(version); err == nil {
			t.Errorf("invalid or overflowing version %q accepted", version)
		}
	}
}
