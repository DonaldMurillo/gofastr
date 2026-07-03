package blueprint

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
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
		ui.Section(ui.SectionConfig{Heading: "Simple, honest pricing", Eyebrow: "Pricing", Description: "", Label: "", Class: "", ID: ""}, ui.Stack(ui.StackConfig{Align: ui.AlignStart}, ui.LinkButton(ui.LinkButtonConfig{Label: "Compare plans", Href: "/pricing", Variant: ui.ButtonPrimary}))),
	)
}

type PricingScreen struct{}

func (s *PricingScreen) ScreenTitle() string        { return "Pricing" }
func (s *PricingScreen) ScreenDescription() string  { return "Plans for teams of every size." }
func (s *PricingScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PricingScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{Title: "Pricing", Subtitle: "Start free. Upgrade when revenue does.", Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "16rem"}, ui.PricingCard(ui.PricingCardConfig{Name: "Starter", Price: "$29", Period: "/mo", Description: "For solo founders finding their first customers.", Features: []string{"Up to 100 customers", "Core billing & invoices", "Email support"}, CTALabel: "Start free", CTAHref: "/signup"}), ui.PricingCard(ui.PricingCardConfig{Name: "Pro", Price: "$99", Period: "/mo", Description: "For growing teams that live in their revenue.", Features: []string{"Unlimited customers", "MRR & churn analytics", "Subscription workflows", "Priority support"}, CTALabel: "Start free", CTAHref: "/signup", Featured: true}), ui.PricingCard(ui.PricingCardConfig{Name: "Scale", Price: "$299", Period: "/mo", Description: "For high-volume revenue and finance teams.", Features: []string{"Everything in Pro", "SSO & audit log", "Dedicated success manager", "99.9% uptime SLA"}, CTALabel: "Contact sales", CTAHref: "/signup"})),
	)
}

type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About Meridian" }
func (s *AboutScreen) ScreenDescription() string  { return "Why we built a calmer billing console." }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("We think billing should feel calm.")),
		render.Tag("p", nil, render.Text("Meridian is a demonstration product built entirely from a GoFastr blueprint — a single declarative file that generates this marketing site, the authenticated console, auth, roles, and an admin back-office, all server-rendered.")),
		render.Tag("p", nil, render.Text("It exists to show that a framework can generate a real, polished web application — not a CRUD scaffold.")),
	)
}

type TermsScreen struct{}

func (s *TermsScreen) ScreenTitle() string        { return "Terms of Service" }
func (s *TermsScreen) ScreenDescription() string  { return "" }
func (s *TermsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TermsScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.Markdown(ui.MarkdownConfig{Source: "# Terms of Service\n\nThis is a demonstration application. The text below is placeholder content that shows long-form, **readable** typography rendered from Markdown in the marketing layout.\n\n## Acceptance\n\nBy using Meridian you agree these terms are illustrative only — there is no real service, billing, or obligation.\n\n## Use of the service\n\n- Evaluate Meridian freely for any purpose.\n- Sample data is reset periodically without notice.\n- Don't rely on it for anything that matters.\n\n## Liability\n\nMeridian is provided *as-is*, without warranty of any kind."}),
	)
}

type PrivacyScreen struct{}

func (s *PrivacyScreen) ScreenTitle() string        { return "Privacy Policy" }
func (s *PrivacyScreen) ScreenDescription() string  { return "" }
func (s *PrivacyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PrivacyScreen) Render() render.HTML {
	return render.Tag("div", nil,
		ui.Markdown(ui.MarkdownConfig{Source: "# Privacy Policy\n\nMeridian is a demo and stores only the sample data you create while exploring it. The content below is placeholder Markdown.\n\n## What we collect\n\nNothing personal. A demo account and any records you add in the console — all reset periodically.\n\n## What we share\n\nNothing. There are no third parties, trackers, or analytics in this demonstration app."}),
	)
}

type LoginScreen struct{ component.ContextOnly }

func (s *LoginScreen) ScreenTitle() string        { return "Sign in" }
func (s *LoginScreen) ScreenDescription() string  { return "" }
func (s *LoginScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LoginScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.AuthCard(ui.AuthCardConfig{Title: "Sign in to Meridian", Alert: blueprintAuthError(ctx), Body: ui.Form(ui.FormConfig{Action: "/auth/login", Method: "POST", SubmitLabel: "Sign in"}, render.Raw("<input type=\"hidden\" name=\"next\" value=\"/app\">"), ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: render.Raw("<input id=\"auth-email\" name=\"email\" type=\"email\" autocomplete=\"email\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: render.Raw("<input id=\"auth-password\" name=\"password\" type=\"password\" autocomplete=\"current-password\" required>")})), Footer: render.Raw("<a href=\"/signup\">Create an account</a>")}),
	)
}

type SignupScreen struct{ component.ContextOnly }

func (s *SignupScreen) ScreenTitle() string        { return "Create your account" }
func (s *SignupScreen) ScreenDescription() string  { return "" }
func (s *SignupScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SignupScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.AuthCard(ui.AuthCardConfig{Title: "Create your Meridian account", Alert: blueprintAuthError(ctx), Body: ui.Form(ui.FormConfig{Action: "/auth/register", Method: "POST", SubmitLabel: "Create account"}, render.Raw("<input type=\"hidden\" name=\"next\" value=\"/app\">"), ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: render.Raw("<input id=\"auth-email\" name=\"email\" type=\"email\" autocomplete=\"email\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: render.Raw("<input id=\"auth-password\" name=\"password\" type=\"password\" autocomplete=\"new-password\" required minlength=\"8\">")})), Footer: render.Raw("<a href=\"/login\">Already have an account? Sign in</a>")}),
	)
}

type DashboardScreen struct{ component.ContextOnly }

func (s *DashboardScreen) ScreenTitle() string        { return "Overview" }
func (s *DashboardScreen) ScreenDescription() string  { return "Your revenue at a glance." }
func (s *DashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{Title: "Overview", Subtitle: "Revenue at a glance", Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "12rem"}, ui.StatCard(ui.StatCardConfig{Label: "MRR", Value: blueprintStatValue(ctx, "subscriptions", "sum", "mrr", "status=active", "money")}), ui.StatCard(ui.StatCardConfig{Label: "Active customers", Value: blueprintStatValue(ctx, "customers", "count", "", "status=active", "")}), ui.StatCard(ui.StatCardConfig{Label: "Past-due invoices", Value: blueprintStatValue(ctx, "invoices", "count", "", "status=past_due", "")}), ui.StatCard(ui.StatCardConfig{Label: "Plans", Value: blueprintStatValue(ctx, "plans", "count", "", "", "")})),
		ui.Card(ui.CardConfig{Heading: "Customers by status"}, ui.BarChart(ui.BarChartConfig{Bars: blueprintGroupBars(ctx, "customers", "status"), ShowLabels: true})),
		blueprintResources["invoices"].WithColumns("number", "customer_id", "amount", "status", "due_on").WithLimit(8).WithHeading("Recent invoices").WithEmpty("No invoices yet.").List(ctx),
	)
}

type CustomersScreen struct{ component.ContextOnly }

func (s *CustomersScreen) ScreenTitle() string        { return "Customers" }
func (s *CustomersScreen) ScreenDescription() string  { return "" }
func (s *CustomersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["customers"].WithColumns("name", "email", "company", "status", "mrr").WithSearch("name").WithLimit(25).WithCreate().WithHeading("Customers").WithEmpty("No customers yet — add your first to get started.").List(ctx),
	)
}

type CustomerDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *CustomerDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *CustomerDetailScreen) ScreenTitle() string           { return "Customer" }
func (s *CustomerDetailScreen) ScreenDescription() string     { return "" }
func (s *CustomerDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *CustomerDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["customers"].Detail(ctx, s.id),
	)
}

type InvoicesScreen struct{ component.ContextOnly }

func (s *InvoicesScreen) ScreenTitle() string        { return "Invoices" }
func (s *InvoicesScreen) ScreenDescription() string  { return "" }
func (s *InvoicesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *InvoicesScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["invoices"].WithColumns("number", "customer_id", "amount", "status", "issued_on", "due_on").WithSearch("number").WithLimit(25).WithCreate().WithHeading("Invoices").WithEmpty("No invoices yet.").List(ctx),
	)
}

type InvoiceDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *InvoiceDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *InvoiceDetailScreen) ScreenTitle() string           { return "Invoice" }
func (s *InvoiceDetailScreen) ScreenDescription() string     { return "" }
func (s *InvoiceDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *InvoiceDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["invoices"].WithTransitions(Transition{Label: "Mark paid", Status: "paid", Variant: "primary", Stamp: "paid_on"}, Transition{Label: "Void", Status: "void", Variant: "danger", Stamp: ""}).Detail(ctx, s.id),
	)
}

type SubscriptionsScreen struct{ component.ContextOnly }

func (s *SubscriptionsScreen) ScreenTitle() string        { return "Subscriptions" }
func (s *SubscriptionsScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SubscriptionsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["subscriptions"].WithColumns("customer_id", "plan_id", "status", "mrr", "renews_on").WithLimit(25).WithCreate().WithHeading("Subscriptions").WithEmpty("No subscriptions yet.").List(ctx),
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
		blueprintResources["subscriptions"].WithTransitions(Transition{Label: "Activate", Status: "active", Variant: "primary", Stamp: ""}, Transition{Label: "Cancel", Status: "canceled", Variant: "danger", Stamp: ""}).Detail(ctx, s.id),
	)
}

type CustomersNewScreen struct{ component.ContextOnly }

func (s *CustomersNewScreen) ScreenTitle() string        { return "New Customer" }
func (s *CustomersNewScreen) ScreenDescription() string  { return "" }
func (s *CustomersNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["customers"].Form(ctx, ""),
	)
}

type CustomersEditScreen struct {
	component.ContextOnly
	id string
}

func (s *CustomersEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *CustomersEditScreen) ScreenTitle() string           { return "Edit Customer" }
func (s *CustomersEditScreen) ScreenDescription() string     { return "" }
func (s *CustomersEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *CustomersEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["customers"].Form(ctx, s.id),
	)
}

type InvoicesNewScreen struct{ component.ContextOnly }

func (s *InvoicesNewScreen) ScreenTitle() string        { return "New Invoice" }
func (s *InvoicesNewScreen) ScreenDescription() string  { return "" }
func (s *InvoicesNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *InvoicesNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["invoices"].Form(ctx, ""),
	)
}

type InvoicesEditScreen struct {
	component.ContextOnly
	id string
}

func (s *InvoicesEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *InvoicesEditScreen) ScreenTitle() string           { return "Edit Invoice" }
func (s *InvoicesEditScreen) ScreenDescription() string     { return "" }
func (s *InvoicesEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *InvoicesEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["invoices"].Form(ctx, s.id),
	)
}

type SubscriptionsNewScreen struct{ component.ContextOnly }

func (s *SubscriptionsNewScreen) ScreenTitle() string        { return "New Subscription" }
func (s *SubscriptionsNewScreen) ScreenDescription() string  { return "" }
func (s *SubscriptionsNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SubscriptionsNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["subscriptions"].Form(ctx, ""),
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
		blueprintResources["subscriptions"].Form(ctx, s.id),
	)
}
