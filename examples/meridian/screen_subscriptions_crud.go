package main

import (
	"context"

	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	subscriptionsCrudTextSubscriptions      = i18n.Define("meridian.screens.subscriptions_crud.subscriptions", "Subscriptions")
	subscriptionsCrudTextNoSubscriptionsYet = i18n.Define("meridian.screens.subscriptions_crud.no_subscriptions_yet", "No subscriptions yet.")
	subscriptionsCrudTextSubscription       = i18n.Define("meridian.screens.subscriptions_crud.subscription", "Subscription")
	subscriptionsCrudTextNewSubscription    = i18n.Define("meridian.screens.subscriptions_crud.new_subscription", "New Subscription")
)

type SubscriptionsScreen struct{ component.ContextOnly }

func (s *SubscriptionsScreen) ScreenTitle(ctx context.Context) string {
	return subscriptionsCrudTextSubscriptions.Text(ctx)
}
func (s *SubscriptionsScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *SubscriptionsScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *SubscriptionsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		appUI.List("subscriptions").Columns("customer_id", "plan_id", "status", "mrr", "renews_on").PageSize(25).Heading(subscriptionsCrudTextSubscriptions.Text(ctx), 1).Empty(subscriptionsCrudTextNoSubscriptionsYet.Text(ctx)).RenderCtx(ctx),
	)
}

type SubscriptionDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *SubscriptionDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *SubscriptionDetailScreen) ScreenTitle(ctx context.Context) string {
	return subscriptionsCrudTextSubscription.Text(ctx)
}
func (s *SubscriptionDetailScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *SubscriptionDetailScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *SubscriptionDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// The record page holds the edit form; the Activate and Cancel
		// buttons come from the entity's States (gofastr.yml), including
		// their variants and from-lists.
		appUI.Record("subscriptions", s.id).Delete().Duplicate().RenderCtx(ctx),
	)
}

type SubscriptionsCreateScreen struct{ component.ContextOnly }

func (s *SubscriptionsCreateScreen) ScreenTitle(ctx context.Context) string {
	return subscriptionsCrudTextNewSubscription.Text(ctx)
}
func (s *SubscriptionsCreateScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *SubscriptionsCreateScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *SubscriptionsCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		appUI.Create("subscriptions").Base("/app/subscriptions").RenderCtx(ctx),
	)
}

func mountSubscriptionsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/subscriptions", &SubscriptionsScreen{}).WithTitle("Subscriptions").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountSubscriptionDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	record := app.NewScreen("/app/subscriptions/:id", &SubscriptionDetailScreen{}).WithTitle("Subscription").WithPolicy(authPolicy("/login", ""))
	record.Intercept = &app.Intercept{From: "/app/subscriptions", As: app.ScreenDrawer}
	site.RegisterScreen(record, appLayout)
}

func mountSubscriptionsCreateScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/subscriptions/create", &SubscriptionsCreateScreen{}).WithTitle("New Subscription").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 12, fn: mountSubscriptionsScreen},
		screenRegistrar{order: 13, fn: mountSubscriptionDetailScreen},
		screenRegistrar{order: 16, fn: mountSubscriptionsCreateScreen},
	)
}
