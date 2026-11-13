package admin

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// droppedNarrowing reports whether r's raw query holds a pair
// url.ParseQuery drops that would have narrowed the screen: one named in
// params whose value does not decode or carries a ';'. A key that does
// not decode names nothing anyone can tell, so it counts too. Without
// this the screen lists wider than the reader asked, with no word; the
// entityui lists warn the same way.
func droppedNarrowing(r *http.Request, params ...string) bool {
	if r == nil || r.URL == nil {
		return false
	}
	for pair := range strings.SplitSeq(r.URL.RawQuery, "&") {
		if _, err := url.ParseQuery(pair); err == nil {
			continue
		}
		// ParseQuery refused this pair: its key or its value does not
		// decode, or it carries a ';'.
		k, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(k)
		if err != nil || strings.Contains(k, ";") || slices.Contains(params, name) {
			return true
		}
	}
	return false
}

// filterNotApplied is the warning a screen draws over a dropped
// narrowing pair: the entityui list's callout, in the same words.
func filterNotApplied(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.EntityFilterInvalidTitle.Text(ctx),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.EntityFilterInvalidBody.Text(ctx)))
}
