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

func droppedNarrowing(r *http.Request, params ...string) bool {
	if r == nil || r.URL == nil {
		return false
	}
	for pair := range strings.SplitSeq(r.URL.RawQuery, "&") {
		if _, err := url.ParseQuery(pair); err == nil {
			continue
		}
		k, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(k)
		if err != nil || strings.Contains(k, ";") || slices.Contains(params, name) {
			return true
		}
	}
	return false
}

func filterNotApplied(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.EntityFilterInvalidTitle.Text(ctx),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.EntityFilterInvalidBody.Text(ctx)))
}
