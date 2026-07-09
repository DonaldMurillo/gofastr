package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Meridian — billing that runs itself" }
func (s *HomeScreen) ScreenDescription() string  { return "The revenue console for modern SaaS." }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.Hero(ui.HeroConfig{Eyebrow: "Billing & revenue", Title: "See your revenue the moment it moves.", Subtitle: "Meridian gives SaaS teams one calm place to manage customers, subscriptions, and invoices — with the metrics that matter, live.", Actions: []render.HTML{ui.LinkButton(ui.LinkButtonConfig{Label: "Start free", Href: "/signup", Variant: ui.ButtonPrimary}), ui.LinkButton(ui.LinkButtonConfig{Label: "See pricing", Href: "/pricing", Variant: ui.ButtonSecondary})}}),
		ui.Section(ui.SectionConfig{Heading: "Everything you need to run revenue", Eyebrow: "Why Meridian", Description: "", Label: "", Class: "", ID: ""}, ui.Grid(ui.GridConfig{Min: "16rem"}, ui.Card(ui.CardConfig{Heading: "Live MRR & churn", Description: "Watch monthly recurring revenue, growth, and churn update as customers sign up and pay."}), ui.Card(ui.CardConfig{Heading: "Subscriptions that flow", Description: "Trialing, active, past-due, canceled — drive the whole lifecycle from one screen."}), ui.Card(ui.CardConfig{Heading: "Invoices, handled", Description: "Open, paid, void — track every invoice and mark them paid in a click."}))),
		// The closing CTA is an "ink band": ui.Themed re-skins this one
		// subtree with the registered dark override (inkBand), so the card
		// paints the dark palette in both color schemes while the rest of
		// the page keeps the canonical theme.
		ui.Themed(inkBand,
			ui.Card(ui.CardConfig{Heading: "Simple, honest pricing", Description: "Start free. Upgrade when revenue does."},
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Wrap: true},
					ui.LinkButton(ui.LinkButtonConfig{Label: "Compare plans", Href: "/pricing", Variant: ui.ButtonPrimary}),
					ui.LinkButton(ui.LinkButtonConfig{Label: "Start free", Href: "/signup", Variant: ui.ButtonSecondary}),
				),
			),
		),
	)
}

// mountHomeScreen mounts the home screen with site.
func mountHomeScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, marketingLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 0, fn: mountHomeScreen})
}
