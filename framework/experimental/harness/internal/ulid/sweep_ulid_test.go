package ulid

import (
	"strings"
	"testing"
)

func TestParseRefusesOverflowAndAcceptsLowercase(t *testing.T) {
	over := "8" + strings.Repeat("0", Length-1)
	if _, err := Parse(over); err == nil {
		t.Error("Parse accepted a ULID above 128 bits")
	}
	if _, _, err := SplitPrefixed("sess_" + over); err == nil {
		t.Error("SplitPrefixed accepted a ULID above 128 bits")
	}
	u := MustNew()
	got, err := Parse(strings.ToLower(u.String()))
	if err != nil || got != u {
		t.Errorf("lowercase Parse = %v, %v; want %v", got, err, u)
	}
}
