package i18nui

import (
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// pseudoLetters maps each ASCII letter to an accented look-alike, so
// text that skipped the catalog stands out as plain English.
var pseudoLetters = map[rune]rune{
	'a': 'á', 'b': 'ƀ', 'c': 'ç', 'd': 'ď', 'e': 'é', 'f': 'ƒ', 'g': 'ĝ',
	'h': 'ĥ', 'i': 'í', 'j': 'ĵ', 'k': 'ķ', 'l': 'ĺ', 'm': 'ɱ', 'n': 'ñ',
	'o': 'ó', 'p': 'þ', 'q': 'ʠ', 'r': 'ŕ', 's': 'š', 't': 'ţ', 'u': 'ú',
	'v': 'ṽ', 'w': 'ŵ', 'x': 'ẋ', 'y': 'ý', 'z': 'ž',
	'A': 'Á', 'B': 'Ɓ', 'C': 'Ç', 'D': 'Ď', 'E': 'É', 'F': 'Ƒ', 'G': 'Ĝ',
	'H': 'Ĥ', 'I': 'Í', 'J': 'Ĵ', 'K': 'Ķ', 'L': 'Ĺ', 'M': 'Ṁ', 'N': 'Ñ',
	'O': 'Ó', 'P': 'Þ', 'Q': 'Ǫ', 'R': 'Ŕ', 'S': 'Š', 'T': 'Ţ', 'U': 'Ú',
	'V': 'Ṽ', 'W': 'Ŵ', 'X': 'Ẋ', 'Y': 'Ý', 'Z': 'Ž',
}

// Pseudo pseudo-localizes s for overflow testing: every letter
// accented, every vowel doubled, the whole bracketed, and padded until
// it is at least a third longer, which is about what German or Spanish
// adds to English. {placeholders} and {{placeholders}} are kept as
// written so interpolation still finds them.
func Pseudo(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < len(s); {
		if s[i] == '{' {
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				for end+i+1 < len(s) && s[i+end+1] == '}' {
					end++
				}
				b.WriteString(s[i : i+end+1])
				i += end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		p, ok := pseudoLetters[r]
		if !ok {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(p)
		if strings.ContainsRune("aeiouAEIOU", r) {
			b.WriteRune(p)
		}
	}
	for utf8.RuneCountInString(b.String())*3 < (utf8.RuneCountInString(s)+1)*4 {
		b.WriteRune('·')
	}
	b.WriteByte(']')
	return b.String()
}

// AddPseudo puts Pseudo of every default into c under tag (en-XA is the
// usual pseudo-locale tag), so a page rendered in that locale shows any
// text that skipped the catalog as plain English and any layout that
// cannot take longer words as overflow.
func AddPseudo(c *i18n.MapCatalog, tag string) {
	for _, k := range slices.Sorted(maps.Keys(Defaults)) {
		c.Set(tag, string(k), i18n.Message{Text: Pseudo(Defaults[k])})
	}
}
