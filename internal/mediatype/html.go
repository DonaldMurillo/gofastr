package mediatype

import (
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/mediatype"
)

// IsHTML recognizes text/html, tolerating malformed or whitespace-delimited parameters.
func IsHTML(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)
	if i := strings.IndexFunc(mediaType, unicode.IsSpace); i >= 0 {
		mediaType = mediaType[:i]
	}
	return mediatype.Fold(mediaType) == "text/html"
}
