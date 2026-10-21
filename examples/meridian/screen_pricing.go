package main

import (
	"context"
	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	pricingScreenTextPricing                 = i18n.Define("meridian.screens.pricing.pricing", "Pricing")
	pricingScreenTextPlansForTeamsOf         = i18n.Define("meridian.screens.pricing.plans_for_teams_of", "Plans for teams of every size.")
	pricingScreenTextStartFreeUpgradeWhen    = i18n.Define("meridian.screens.pricing.start_free_upgrade_when", "Start free. Upgrade when revenue does.")
	pricingScreenTextUpTo100Customers        = i18n.Define("meridian.screens.pricing.up_to_100_customers", "Up to 100 customers")
	pricingScreenTextCoreBillingInvoices     = i18n.Define("meridian.screens.pricing.core_billing_invoices", "Core billing & invoices")
	pricingScreenTextEmailSupport            = i18n.Define("meridian.screens.pricing.email_support", "Email support")
	pricingScreenTextStarter                 = i18n.Define("meridian.screens.pricing.starter", "Starter")
	pricingScreenTextMo                      = i18n.Define("meridian.screens.pricing.mo", "/mo")
	pricingScreenTextForSoloFoundersFinding  = i18n.Define("meridian.screens.pricing.for_solo_founders_finding", "For solo founders finding their first customers.")
	pricingScreenTextStartFree               = i18n.Define("meridian.screens.pricing.start_free", "Start free")
	pricingScreenTextUnlimitedCustomers      = i18n.Define("meridian.screens.pricing.unlimited_customers", "Unlimited customers")
	pricingScreenTextMrrChurnAnalytics       = i18n.Define("meridian.screens.pricing.mrr_churn_analytics", "MRR & churn analytics")
	pricingScreenTextSubscriptionWorkflows   = i18n.Define("meridian.screens.pricing.subscription_workflows", "Subscription workflows")
	pricingScreenTextPrioritySupport         = i18n.Define("meridian.screens.pricing.priority_support", "Priority support")
	pricingScreenTextPro                     = i18n.Define("meridian.screens.pricing.pro", "Pro")
	pricingScreenTextForGrowingTeamsThat     = i18n.Define("meridian.screens.pricing.for_growing_teams_that", "For growing teams that live in their revenue.")
	pricingScreenTextEverythingInPro         = i18n.Define("meridian.screens.pricing.everything_in_pro", "Everything in Pro")
	pricingScreenTextSsoAuditLog             = i18n.Define("meridian.screens.pricing.sso_audit_log", "SSO & audit log")
	pricingScreenTextDedicatedSuccessManager = i18n.Define("meridian.screens.pricing.dedicated_success_manager", "Dedicated success manager")
	pricingScreenTextN999UptimeSla           = i18n.Define("meridian.screens.pricing.n99_9_uptime_sla", "99.9% uptime SLA")
	pricingScreenTextScale                   = i18n.Define("meridian.screens.pricing.scale", "Scale")
	pricingScreenTextForHighVolumeRevenue    = i18n.Define("meridian.screens.pricing.for_high_volume_revenue", "For high-volume revenue and finance teams.")
	pricingScreenTextContactSales            = i18n.Define("meridian.screens.pricing.contact_sales", "Contact sales")
)

type PricingScreen struct{ component.ContextOnly }

func (s *PricingScreen) ScreenTitle(ctx context.Context) string {
	return pricingScreenTextPricing.Text(ctx)
}
func (s *PricingScreen) ScreenDescription(ctx context.Context) string {
	return pricingScreenTextPlansForTeamsOf.Text(ctx)
}
func (s *PricingScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// The page's share card: its Open Graph title and description.
var (
	pricingOGTitle       = i18n.Define("meridian.screens.pricing.og_title", "Meridian pricing: start free")
	pricingOGDescription = i18n.Define("meridian.screens.pricing.og_description", "Plans for teams of every size. Upgrade when revenue does.")
)

// ScreenSEO bundles this page's whole SEO declaration: canonical (the
// page is reachable with tracking params), a page-specific OG override
// of the sitewide default, and Product JSON-LD for the Pro plan.
func (s *PricingScreen) ScreenSEO(ctx context.Context) uihost.SEO {
	pro := seo.NewProduct()
	pro.Name = "Meridian Pro"
	pro.Description = pricingScreenTextForGrowingTeamsThat.Text(ctx)
	offer := seo.NewOffer()
	offer.Price = "99"
	offer.PriceCurrency = "USD"
	offer.URL = siteOrigin + "/pricing"
	pro.Offers = &offer
	return uihost.SEO{
		Canonical: siteOrigin + "/pricing",
		OG: &uihost.OG{
			Title:       pricingOGTitle.Text(ctx),
			Description: pricingOGDescription.Text(ctx),
			URL:         siteOrigin + "/pricing",
			Type:        "website",
		},
		Schema: []seo.Thing{pro},
	}
}

func (s *PricingScreen) RenderCtx(ctx context.Context) render.HTML {
	// The screen owns its rhythm: the page frame (marketingLayout)
	// supplies only the measure; the stack's gap spaces the blocks.
	// html.Div root: the pack reader (reverseRenderBody) accepts a
	// tag-primitive root; the stack inside owns the rhythm.
	return html.Div(html.DivConfig{}, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
		ui.PageHeader(ui.PageHeaderConfig{Title: pricingScreenTextPricing.Text(ctx), Subtitle: pricingScreenTextStartFreeUpgradeWhen.Text(ctx), Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "16rem"}, ui.PricingCard(ui.PricingCardConfig{Name: pricingScreenTextStarter.Text(ctx), HeadingLevel: 2, Price: "$29", Period: pricingScreenTextMo.Text(ctx), Description: pricingScreenTextForSoloFoundersFinding.Text(ctx), Features: []string{pricingScreenTextUpTo100Customers.Text(ctx), pricingScreenTextCoreBillingInvoices.Text(ctx), pricingScreenTextEmailSupport.Text(ctx)}, CTALabel: pricingScreenTextStartFree.Text(ctx), CTAHref: "/signup"}), ui.PricingCard(ui.PricingCardConfig{Name: pricingScreenTextPro.Text(ctx), HeadingLevel: 2, Price: "$99", Period: pricingScreenTextMo.Text(ctx), Description: pricingScreenTextForGrowingTeamsThat.Text(ctx), Features: []string{pricingScreenTextUnlimitedCustomers.Text(ctx), pricingScreenTextMrrChurnAnalytics.Text(ctx), pricingScreenTextSubscriptionWorkflows.Text(ctx), pricingScreenTextPrioritySupport.Text(ctx)}, CTALabel: pricingScreenTextStartFree.Text(ctx), CTAHref: "/signup", Featured: true}), ui.PricingCard(ui.PricingCardConfig{Name: pricingScreenTextScale.Text(ctx), HeadingLevel: 2, Price: "$299", Period: pricingScreenTextMo.Text(ctx), Description: pricingScreenTextForHighVolumeRevenue.Text(ctx), Features: []string{pricingScreenTextEverythingInPro.Text(ctx), pricingScreenTextSsoAuditLog.Text(ctx), pricingScreenTextDedicatedSuccessManager.Text(ctx), pricingScreenTextN999UptimeSla.Text(ctx)}, CTALabel: pricingScreenTextContactSales.Text(ctx), CTAHref: "/signup"})),
	))
}

// mountPricingScreen mounts the pricing screen with site.
func mountPricingScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/pricing", &PricingScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 1, fn: mountPricingScreen})
}
