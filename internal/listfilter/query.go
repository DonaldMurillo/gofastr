// Package listfilter handles dropped narrowing query pairs and their warning.
package listfilter

import (
	"context"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// DroppedNarrowing reports whether ParseQuery rejected a narrowing pair or a pair with an unreadable key.
func DroppedNarrowing(rawQuery string, narrows func(string) bool) bool {
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if _, err := url.ParseQuery(pair); err == nil {
			continue
		}
		key, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(key)
		if err != nil || strings.Contains(key, ";") || narrows(name) {
			return true
		}
	}
	return false
}

// Warning renders the localized notice for a filter that could not be applied.
func Warning(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.EntityFilterInvalidTitle.Text(ctx),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.EntityFilterInvalidBody.Text(ctx)))
}
