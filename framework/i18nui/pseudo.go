package i18nui

import (
	"context"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

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

// Pseudo pseudo-localizes s for overflow testing: every letter accented,
// every vowel doubled, the whole bracketed and padded to at least a third
// longer, with {placeholders} and {{placeholders}} kept as written.
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

const pseudoKey = "i18nui.pseudo"

// AddPseudo puts Pseudo of every defined phrase ([i18n.Phrases]) into c
// under tag (usually en-XA), and pseudo-localizes the entity label
// fallbacks in that locale too.
func AddPseudo(c *i18n.MapCatalog, tag string) {
	phrases := i18n.Phrases()
	for _, k := range slices.Sorted(maps.Keys(phrases)) {
		c.Set(tag, k, i18n.Message{Text: Pseudo(phrases[k])})
	}
	c.Set(tag, pseudoKey, i18n.Message{Text: "true"})
}

func fallback(ctx context.Context, tr *i18n.Translator, s string) string {
	if s == "" {
		return s
	}
	if v, ok := catalogString(ctx, tr, pseudoKey); ok && v == "true" {
		return Pseudo(s)
	}
	return s
}
