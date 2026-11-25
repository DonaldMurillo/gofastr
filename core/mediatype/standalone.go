package mediatype

import (
	_ "embed"
	"strings"
)

//go:embed fold.go
var foldSource string

// StandaloneFold returns a private foldMediaType declaration for generated Go code.
// The declaration requires the strings import.
func StandaloneFold() string {
	_, body, _ := strings.Cut(foldSource, "\nfunc Fold(")
	return "func foldMediaType(" + body
}
