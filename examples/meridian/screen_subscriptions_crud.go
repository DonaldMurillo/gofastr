package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type SubscriptionsScreen struct{ component.ContextOnly }

func (s *SubscriptionsScreen) ScreenTitle() string        { return "Subscriptions" }
func (s *SubscriptionsScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SubscriptionsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["subscriptions"].WithColumns("customer_id", "plan_id", "status", "mrr", "renews_on").WithLimit(25).WithCreate().WithHeading("Subscriptions").WithEmpty("No subscriptions yet.").List(ctx),
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
	return render.Tag("div", nil,
		appResources["subscriptions"].WithTransitions(Transition{Label: "Activate", Status: "active", Variant: "primary", Stamp: ""}, Transition{Label: "Cancel", Status: "canceled", Variant: "danger", Stamp: ""}).Detail(ctx, s.id),
	)
}

type SubscriptionsNewScreen struct{ component.ContextOnly }

func (s *SubscriptionsNewScreen) ScreenTitle() string        { return "New Subscription" }
func (s *SubscriptionsNewScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SubscriptionsNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["subscriptions"].Form(ctx, ""),
	)
}

type SubscriptionsEditScreen struct {
	component.ContextOnly
	id string
}

func (s *SubscriptionsEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *SubscriptionsEditScreen) ScreenTitle() string           { return "Edit Subscription" }
func (s *SubscriptionsEditScreen) ScreenDescription() string     { return "" }
func (s *SubscriptionsEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *SubscriptionsEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["subscriptions"].Form(ctx, s.id),
	)
}

func mountSubscriptionsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["subscriptions"] = ResourceConfig{
		Title: "Subscriptions", Singular: "Subscription", BasePath: "/app/subscriptions", APIPath: "/api/subscriptions",
		Crud:    fwApp.MustCrudHandler("subscriptions"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "customer_id", Label: "Customer", Type: "relation"},
			{Key: "plan_id", Label: "Plan", Type: "relation"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Key: "mrr", Label: "MRR", Type: "decimal"},
			{Key: "started_on", Label: "Started", Type: "date"},
			{Key: "renews_on", Label: "Renews", Type: "date"},
		},
		Relations: map[string]RelSource{
			"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
			"plan_id":     {Crud: fwApp.MustCrudHandler("plans"), Display: "name"},
		},
	}
	site.RegisterScreen(app.NewScreen("/app/subscriptions", &SubscriptionsScreen{}).WithTitle("Subscriptions").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountSubscriptionDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/subscriptions/:id", &SubscriptionDetailScreen{}).WithTitle("Subscription").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountSubscriptionsNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/subscriptions/new", &SubscriptionsNewScreen{}).WithTitle("New Subscription").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountSubscriptionsEditScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/subscriptions/:id/edit", &SubscriptionsEditScreen{}).WithTitle("Edit Subscription").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 12, fn: mountSubscriptionsScreen},
		screenRegistrar{order: 13, fn: mountSubscriptionDetailScreen},
		screenRegistrar{order: 18, fn: mountSubscriptionsNewScreen},
		screenRegistrar{order: 19, fn: mountSubscriptionsEditScreen},
	)
}
