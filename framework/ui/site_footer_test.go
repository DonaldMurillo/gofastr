package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSiteFooterRendersLeadColumnsAndBottom(t *testing.T) {
	h := string(SiteFooter(SiteFooterConfig{
		Lead: render.Raw(`<div class="brand">GoFastr</div>`),
		Columns: []SiteFooterColumn{
			{Title: "Read", Links: []SiteFooterLink{
				{Label: "Docs", Href: "/docs/"},
				{Label: "GitHub", Href: "https://gh", External: true},
			}},
			{Title: "Use", Links: []SiteFooterLink{
				{Label: "Kiln", Href: "/kiln"},
			}},
		},
		Bottom: []render.HTML{render.Raw(`<span>© 2026</span>`)},
	}))

	for _, want := range []string{
		`data-fui-comp="ui-site-footer"`,
		`class="ui-site-footer__lead"`,
		`<div class="brand">GoFastr</div>`,
		`class="ui-site-footer__col-title"`,
		`>Read<`,
		`>Use<`,
		`href="/docs/"`,
		`href="https://gh"`,
		`target="_blank"`,
		`rel="external"`,
		`<span>© 2026</span>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("SiteFooter missing %q\nhtml=%s", want, h)
		}
	}
}

func TestSiteFooterInternalLinkOmitsTargetBlank(t *testing.T) {
	h := string(SiteFooter(SiteFooterConfig{
		Columns: []SiteFooterColumn{
			{Title: "Read", Links: []SiteFooterLink{
				{Label: "Docs", Href: "/docs/"},
			}},
		},
	}))
	if strings.Contains(h, `target="_blank"`) {
		t.Errorf("internal link (External:false) must NOT open in new tab:\n%s", h)
	}
	if strings.Contains(h, `rel="external"`) {
		t.Errorf("internal link (External:false) must NOT have rel=external:\n%s", h)
	}
}

func TestSiteFooterWithoutBottomOmitsBottomStrip(t *testing.T) {
	h := string(SiteFooter(SiteFooterConfig{
		Columns: []SiteFooterColumn{
			{Title: "x", Links: []SiteFooterLink{{Label: "a", Href: "/a"}}},
		},
	}))
	if strings.Contains(h, "ui-site-footer__bottom") {
		t.Errorf("empty Bottom must not emit the bottom strip element:\n%s", h)
	}
}

func TestSiteFooterWithoutLeadOmitsLeadSlot(t *testing.T) {
	h := string(SiteFooter(SiteFooterConfig{
		Columns: []SiteFooterColumn{
			{Title: "x", Links: []SiteFooterLink{{Label: "a", Href: "/a"}}},
		},
	}))
	if strings.Contains(h, "ui-site-footer__lead") {
		t.Errorf("Lead slot should be omitted when nil:\n%s", h)
	}
}
