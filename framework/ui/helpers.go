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

var autoIDCounter int64

func scopedID(ctx context.Context, prefix string, children ...string) string {
	if render.HasIDScope(ctx) {
		return render.UniqueID(ctx, prefix, children...)
	}
	return autoID(prefix)
}

func autoID(prefix string) string {
	n := atomic.AddInt64(&autoIDCounter, 1)
	return prefix + "-" + strconv.FormatInt(n, 36)
}
