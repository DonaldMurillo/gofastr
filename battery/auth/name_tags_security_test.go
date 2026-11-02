package auth

import (
	"errors"
	"testing"
)

// A display name is drawn on other people's screens. Tag characters
// (U+E0000–U+E007F) spell text nobody sees and a Hangul filler draws a
// letter-wide blank, so CleanName refuses both; a subdivision flag,
// whose tag letters are the flag, stays a valid name.
func TestCleanNameTagsAndHangulFillers(t *testing.T) {
	scotland := "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"
	if got, err := CleanName("Ana " + scotland); err != nil || got != "Ana "+scotland {
		t.Errorf("CleanName(flag) = %q, %v; want it kept", got, err)
	}
	for _, name := range []string{
		"Ana\U000E0069\U000E0067\U000E006E", // hidden "ign"
		"ㅤ",                            // Hangul filler alone
		"Aﾠna",
	} {
		if _, err := CleanName(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CleanName(%q) err = %v, want ErrInvalidName", name, err)
		}
	}
}

// CanonicalEmail strips the same set, so a tag-salted twin folds onto
// the base account.
func TestCanonicalEmailStripsTagCharacters(t *testing.T) {
	got, err := CanonicalEmail("ow\U000E0078nerㅤ@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "owner@example.com" {
		t.Errorf("CanonicalEmail = %q, want owner@example.com", got)
	}
}
