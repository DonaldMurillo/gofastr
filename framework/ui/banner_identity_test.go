package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestBannersRenderTheirID(t *testing.T) {
	if h := string(Banner(BannerConfig{Title: "T", ID: "promo"})); !strings.Contains(h, ` id="promo"`) {
		t.Errorf("Banner ID is not the root's id:\n%s", h)
	}
	if h := string(NetworkRetryBanner(NetworkRetryBannerConfig{HealthEndpoint: "/health", ID: "offline"})); !strings.Contains(h, ` id="offline"`) {
		t.Errorf("NetworkRetryBanner ID is not the root's id:\n%s", h)
	}
}

var tooltipPopID = regexp.MustCompile(`role="tooltip"[^>]*\sid="([^"]*)"|\sid="([^"]*)"[^>]*role="tooltip"`)

func tooltipID(t *testing.T, h render.HTML) string {
	t.Helper()
	m := tooltipPopID.FindStringSubmatch(string(h))
	if m == nil {
		t.Fatalf("no tooltip pop id in %s", h)
	}
	id := m[1] + m[2]
	if !strings.Contains(string(h), `aria-describedby="`+id+`"`) {
		t.Fatalf("trigger does not name pop %q: %s", id, h)
	}
	return id
}

func TestTooltipsGetDistinctIDs(t *testing.T) {
	page := render.WithIDScope(context.Background())
	trigger := render.HTML(`<button>?</button>`)
	a := tooltipID(t, Tooltip(TooltipConfig{Text: "Need help?", Ctx: page}, trigger))
	b := tooltipID(t, Tooltip(TooltipConfig{Text: "Need help?", Ctx: page}, trigger))
	if a == b {
		t.Errorf("two tooltips with one Text on a page share id %q", a)
	}
	if up, down := tooltipID(t, Tooltip(TooltipConfig{Text: "👍"}, trigger)), tooltipID(t, Tooltip(TooltipConfig{Text: "👎"}, trigger)); up == down {
		t.Errorf("tooltips with different Text share id %q", up)
	}
}
