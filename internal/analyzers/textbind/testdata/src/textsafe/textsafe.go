package textsafe

import (
	"strings"
	"unicode/utf8"
)

func Storable(value string) bool { // want Storable:`check=1`
	return utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}
