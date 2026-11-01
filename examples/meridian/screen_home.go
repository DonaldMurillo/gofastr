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
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	homeScreenTextMeridianBillingThatRuns      = i18n.Define("meridian.screens.home.meridian_billing_that_runs", "Meridian: billing that runs itself")
	homeScreenTextTheRevenueConsoleFor         = i18n.Define("meridian.screens.home.the_revenue_console_for", "The revenue console for modern SaaS.")
	homeScreenTextStartFree                    = i18n.Define("meridian.screens.home.start_free", "Start free")
	homeScreenTextSeePricing                   = i18n.Define("meridian.screens.home.see_pricing", "See pricing")
	homeScreenTextBillingRevenue               = i18n.Define("meridian.screens.home.billing_revenue", "Billing & revenue")
	homeScreenTextSeeYourRevenueThe            = i18n.Define("meridian.screens.home.see_your_revenue_the", "See your revenue the moment it moves.")
	homeScreenTextMeridianGivesSaasTeams       = i18n.Define("meridian.screens.home.meridian_gives_saas_teams", "Meridian gives SaaS teams one calm place to manage customers, subscriptions, and invoices, with the metrics that matter, live.")
	homeScreenTextEverythingYouNeedTo          = i18n.Define("meridian.screens.home.everything_you_need_to", "Everything you need to run revenue")
	homeScreenTextWhyMeridian                  = i18n.Define("meridian.screens.home.why_meridian", "Why Meridian")
	homeScreenTextLiveMrrChurn                 = i18n.Define("meridian.screens.home.live_mrr_churn", "Live MRR & churn")
	homeScreenTextWatchMonthlyRecurringRevenue = i18n.Define("meridian.screens.home.watch_monthly_recurring_revenue", "Watch monthly recurring revenue, growth, and churn update as customers sign up and pay.")
	homeScreenTextSubscriptionsThatFlow        = i18n.Define("meridian.screens.home.subscriptions_that_flow", "Subscriptions that flow")
	homeScreenTextTrialingActivePastDue        = i18n.Define("meridian.screens.home.trialing_active_past_due", "Trialing, active, past-due, canceled: drive the whole lifecycle from one screen.")
	homeScreenTextInvoicesHandled              = i18n.Define("meridian.screens.home.invoices_handled", "Invoices, handled")
	homeScreenTextOpenPaidVoidTrack            = i18n.Define("meridian.screens.home.open_paid_void_track", "Open, paid, void: track every invoice and mark them paid in a click.")
	homeScreenTextSimpleHonestPricing          = i18n.Define("meridian.screens.home.simple_honest_pricing", "Simple, honest pricing")
	homeScreenTextStartFreeUpgrade             = i18n.Define("meridian.screens.home.start_free_upgrade", "Start free. Upgrade when revenue does.")
	homeScreenTextComparePlans                 = i18n.Define("meridian.screens.home.compare_plans", "Compare plans")
)

type HomeScreen struct{ component.ContextOnly }

func (s *HomeScreen) ScreenTitle(ctx context.Context) string {
	return homeScreenTextMeridianBillingThatRuns.Text(ctx)
}
func (s *HomeScreen) ScreenDescription(ctx context.Context) string {
	return homeScreenTextTheRevenueConsoleFor.Text(ctx)
}
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// ScreenSchema emits Organization + WebSite JSON-LD on the homepage,
// the structured-data anchor crawlers use to connect the brand's name,
// logo, and canonical URL.
func (s *HomeScreen) ScreenSchema(ctx context.Context) []seo.Thing {
	org := seo.NewOrganization()
	org.Name = "Meridian"
	org.URL = siteOrigin
	site := seo.NewWebSite()
	site.Name = "Meridian"
	site.URL = siteOrigin
	return []seo.Thing{org, site}
}

func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {
	// The screen owns its rhythm: the page frame (marketingLayout)
	// supplies only the measure; the stack's gap spaces the blocks.
	// html.Div root: the pack reader (reverseRenderBody) accepts a
	// tag-primitive root; the stack inside owns the rhythm.
	return html.Div(html.DivConfig{}, ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
		ui.Hero(ui.HeroConfig{Eyebrow: homeScreenTextBillingRevenue.Text(ctx), Title: homeScreenTextSeeYourRevenueThe.Text(ctx), Subtitle: homeScreenTextMeridianGivesSaasTeams.Text(ctx), Actions: []render.HTML{ui.LinkButton(ui.LinkButtonConfig{Label: homeScreenTextStartFree.Text(ctx), Href: "/signup", Variant: ui.ButtonPrimary}), ui.LinkButton(ui.LinkButtonConfig{Label: homeScreenTextSeePricing.Text(ctx), Href: "/pricing", Variant: ui.ButtonSecondary})}}),
		ui.Section(ui.SectionConfig{Ctx: ctx, Heading: homeScreenTextEverythingYouNeedTo.Text(ctx), Eyebrow: homeScreenTextWhyMeridian.Text(ctx), Description: "", Label: "", Class: "", ID: ""}, ui.Grid(ui.GridConfig{Min: "16rem"}, ui.Card(ui.CardConfig{Heading: homeScreenTextLiveMrrChurn.Text(ctx), Description: homeScreenTextWatchMonthlyRecurringRevenue.Text(ctx)}), ui.Card(ui.CardConfig{Heading: homeScreenTextSubscriptionsThatFlow.Text(ctx), Description: homeScreenTextTrialingActivePastDue.Text(ctx)}), ui.Card(ui.CardConfig{Heading: homeScreenTextInvoicesHandled.Text(ctx), Description: homeScreenTextOpenPaidVoidTrack.Text(ctx)}))),
		// The closing CTA is an "ink band": ui.Themed re-skins this one
		// subtree with the registered dark override (inkBand), so the card
		// paints the dark palette in both color schemes while the rest of
		// the page keeps the canonical theme.
		ui.Themed(inkBand,
			ui.Card(ui.CardConfig{Heading: homeScreenTextSimpleHonestPricing.Text(ctx), Description: homeScreenTextStartFreeUpgrade.Text(ctx)},
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
					ui.LinkButton(ui.LinkButtonConfig{Label: homeScreenTextComparePlans.Text(ctx), Href: "/pricing", Variant: ui.ButtonPrimary}),
					ui.LinkButton(ui.LinkButtonConfig{Label: homeScreenTextStartFree.Text(ctx), Href: "/signup", Variant: ui.ButtonSecondary}),
				),
			),
		),
	))
}

// mountHomeScreen mounts the home screen with site.
func mountHomeScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 0, fn: mountHomeScreen})
}
