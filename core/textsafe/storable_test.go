package textsafe

import "testing"

func TestStorable(t *testing.T) {
	for _, value := range []string{"", "plain", "café 東京 🐈", "line\n\tbreak", "\u202e", "�"} {
		if !Storable(value) {
			t.Errorf("Storable(%q) = false", value)
		}
	}
	for _, value := range []string{"\x00", "a\x00b", "bad\xff", "\xc0\x80", "\xed\xa0\x80", "\xed\xbf\xbf"} {
		if Storable(value) {
			t.Errorf("Storable(%q) = true", value)
		}
	}
}
