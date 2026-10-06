package main

import (
	"context"

	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SubscriptionsScreen struct{ component.ContextOnly }

func (s *SubscriptionsScreen) ScreenTitle() string        { return "Subscriptions" }
func (s *SubscriptionsScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SubscriptionsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		appUI.List("subscriptions").Columns("customer_id", "plan_id", "status", "mrr", "renews_on").PageSize(25).Heading("Subscriptions", 1).Empty("No subscriptions yet.").RenderCtx(ctx),
	)
}

type SubscriptionDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *SubscriptionDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *SubscriptionDetailScreen) ScreenTitle() string           { return "Subscription" }
func (s *SubscriptionDetailScreen) ScreenDescription() string     { return "" }
func (s *SubscriptionDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *SubscriptionDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// The record page holds the edit form; the Activate and Cancel
		// buttons come from the entity's States (gofastr.yml), including
		// their variants and from-lists.
		appUI.Record("subscriptions", s.id).Delete().Duplicate().RenderCtx(ctx),
	)
}

type SubscriptionsCreateScreen struct{ component.ContextOnly }

func (s *SubscriptionsCreateScreen) ScreenTitle() string        { return "New Subscription" }
func (s *SubscriptionsCreateScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsCreateScreen) ScreenType() app.ScreenType { return app.ScreenPage }

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
