package mediatype

import "strings"

// Fold normalizes the case of a media type without validating or removing parameters.
func Fold(value string) string {
	return strings.ToLower(value)
}
