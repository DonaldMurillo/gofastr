package mediatype

import (
	_ "embed"
	"strings"
)

//go:embed fold.go
var foldSource string

// StandaloneFold returns the Fold declaration for generated Go code.
// The declaration requires the strings import.
func StandaloneFold() string {
	_, declaration, _ := strings.Cut(foldSource, "\n// Fold ")
	return "// Fold " + declaration
}

// Fold normalizes the case of a media type without validating or removing parameters.
func Fold(value string) string {
	return strings.ToLower(value)
}
