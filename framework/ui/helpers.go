package ui

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// serializeExtraAttrs renders already-sanitized extras for string-built
// roots: one leading space per attribute, keys sorted so SSR output is
// deterministic (map ranging would reorder attributes render to render).
func serializeExtraAttrs(attrs html.Attrs) string {
	if len(attrs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, k := range slices.Sorted(maps.Keys(attrs)) {
		if a := render.Attr(k, attrs[k]); a != "" {
			sb.WriteByte(' ')
			sb.WriteString(a)
		}
	}
	return sb.String()
}

// autoIDCounter provides unique IDs for UI components.
var autoIDCounter int64

// autoID generates a unique ID with the given prefix.
func autoID(prefix string) string {
	n := atomic.AddInt64(&autoIDCounter, 1)
	return prefix + "-" + strconv.FormatInt(n, 36)
}
