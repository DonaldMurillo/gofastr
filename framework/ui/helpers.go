package ui

import (
	"context"
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

// scopedID derives an element id for a component given none: from
// ctx's id scope when it has one (render.UniqueID, "rep", "rep-2", or
// "cart--rep" inside island cart), so the id is the same on the page
// and in the island's RPC answer; from autoID's process-wide counter
// for a render with no scope. children are the suffixes of the ids the
// component builds from it (render.UniqueID).
func scopedID(ctx context.Context, prefix string, children ...string) string {
	if render.HasIDScope(ctx) {
		return render.UniqueID(ctx, prefix, children...)
	}
	return autoID(prefix)
}

// autoID generates a unique ID with the given prefix. Prefer scopedID:
// a counter value differs between two renders of the same island.
func autoID(prefix string) string {
	n := atomic.AddInt64(&autoIDCounter, 1)
	return prefix + "-" + strconv.FormatInt(n, 36)
}
