package asciicase

import (
	"strings"
	"testing"
)

func TestLowerFoldsASCIIOnly(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"kiosk", "kiosk"},
		{"KIOSK", "kiosk"},
		{"Order_Items-2", "order_items-2"},
		{"AZ@[`az", "az@[`az"},
		{"zebra Zone", "zebra zone"},
		// The runes Unicode case mapping lands on ASCII keep their bytes.
		{"Kiosk", "Kiosk"},
		{"GİF", "gİf"},
		{"ſVG", "ſvg"},
		{"ıD", "ıd"},
	} {
		if got := Lower(tc.in); got != tc.want {
			t.Errorf("Lower(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// For ASCII input Lower agrees with strings.ToLower, so a switch from
// one to the other changes nothing for an ASCII name.
func TestLowerMatchesToLowerOnASCII(t *testing.T) {
	var b strings.Builder
	for c := 0; c < 128; c++ {
		b.WriteByte(byte(c))
	}
	s := b.String()
	if got, want := Lower(s), strings.ToLower(s); got != want {
		t.Errorf("Lower and strings.ToLower differ on ASCII:\n%q\n%q", got, want)
	}
}
