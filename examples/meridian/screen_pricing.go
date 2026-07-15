package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type PricingScreen struct{}

func (s *PricingScreen) ScreenTitle() string        { return "Pricing" }
func (s *PricingScreen) ScreenDescription() string  { return "Plans for teams of every size." }
func (s *PricingScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenSEO bundles this page's whole SEO declaration: canonical (the
// page is reachable with tracking params), a page-specific OG override
// of the sitewide default, and Product JSON-LD for the Pro plan.
func (s *PricingScreen) ScreenSEO() uihost.SEO {
	pro := seo.NewProduct()
	pro.Name = "Meridian Pro"
	pro.Description = "For growing teams that live in their revenue."
	offer := seo.NewOffer()
	offer.Price = "99"
	offer.PriceCurrency = "USD"
	offer.URL = "https://meridian.gofastr.dev/pricing"
	pro.Offers = &offer
	return uihost.SEO{
		Canonical: "https://meridian.gofastr.dev/pricing",
		OG: &uihost.OG{
			Title:       "Meridian pricing — start free",
			Description: "Plans for teams of every size. Upgrade when revenue does.",
			URL:         "https://meridian.gofastr.dev/pricing",
			Type:        "website",
		},
		Schema: []seo.Thing{pro},
	}
}

func (s *PricingScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{Title: "Pricing", Subtitle: "Start free. Upgrade when revenue does.", Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "16rem"}, ui.PricingCard(ui.PricingCardConfig{Name: "Starter", HeadingLevel: 2, Price: "$29", Period: "/mo", Description: "For solo founders finding their first customers.", Features: []string{"Up to 100 customers", "Core billing & invoices", "Email support"}, CTALabel: "Start free", CTAHref: "/signup"}), ui.PricingCard(ui.PricingCardConfig{Name: "Pro", HeadingLevel: 2, Price: "$99", Period: "/mo", Description: "For growing teams that live in their revenue.", Features: []string{"Unlimited customers", "MRR & churn analytics", "Subscription workflows", "Priority support"}, CTALabel: "Start free", CTAHref: "/signup", Featured: true}), ui.PricingCard(ui.PricingCardConfig{Name: "Scale", HeadingLevel: 2, Price: "$299", Period: "/mo", Description: "For high-volume revenue and finance teams.", Features: []string{"Everything in Pro", "SSO & audit log", "Dedicated success manager", "99.9% uptime SLA"}, CTALabel: "Contact sales", CTAHref: "/signup"})),
	)
}

// mountPricingScreen mounts the pricing screen with site.
func mountPricingScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/pricing", &PricingScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 1, fn: mountPricingScreen})
}
