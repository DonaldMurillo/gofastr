package main

import (
	"context"
	"database/sql"
	"testing"

	// The owner extractor is installed by battery/auth's package init;
	// the billing entities are owner-scoped, so the test needs it.
	_ "github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/examples/meridian/entities"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

type lifecycleActor struct{ id string }

func (a lifecycleActor) GetID() string      { return a.id }
func (a lifecycleActor) GetEmail() string   { return "ops@meridian.dev" }
func (a lifecycleActor) GetRoles() []string { return nil }

// lifecycleApp boots the entity registry on an in-memory database as one
// owner, with the relation targets a billing row names already seeded.
func lifecycleApp(t *testing.T) (context.Context, *framework.App) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	entities.RegisterAll(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ctx := handler.SetUser(context.Background(), lifecycleActor{id: "u1"})
	customer, err := entities.NewCustomersRepo(app).Create(ctx, entities.CustomersInput{Name: "Acme", Email: "acme@example.io"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	plan, err := entities.NewPlansRepo(app).Create(ctx, entities.PlansInput{Name: "Pro", Slug: "pro", Price: "99"})
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	t.Helper()
	return context.WithValue(ctx, lifecycleFixtures{}, fixtures{customer: customer.ID, plan: plan.ID}), app
}

type lifecycleFixtures struct{}

type fixtures struct{ customer, plan string }

func lifecycleFixturesFrom(ctx context.Context) fixtures {
	f, _ := ctx.Value(lifecycleFixtures{}).(fixtures)
	return f
}

// The invoice lifecycle is enforced at the CRUD layer, not advised: an
// invoice is born draft, `issue` opens it, and mark_paid accepts only an
// issued or past-due invoice — paying a draft was the workflow hole the
// enforced from-lists close (issue #669, Meridian).
func TestInvoiceLifecycleIsEnforced(t *testing.T) {
	ctx, app := lifecycleApp(t)
	f := lifecycleFixturesFrom(ctx)
	invoices := entities.NewInvoicesRepo(app)
	inv, err := invoices.Create(ctx, entities.InvoicesInput{CustomerId: f.customer, Number: "T-1", Amount: "10"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if inv.Status == nil || *inv.Status != entities.InvoicesStatusDraft {
		t.Fatalf("a fresh invoice starts draft: %+v", inv)
	}
	bad := entities.InvoicesStatusPaid
	if _, err := invoices.Create(ctx, entities.InvoicesInput{CustomerId: f.customer, Number: "T-2", Amount: "10", Status: &bad}); err == nil {
		t.Fatal("a create must not mint a paid invoice: status moves only through transitions")
	}
	if _, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveMarkPaid); err == nil {
		t.Fatal("mark_paid from draft must be refused: pay an issued invoice, not a draft")
	}
	issued, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveIssue)
	if err != nil || issued.Status == nil || *issued.Status != entities.InvoicesStatusOpen {
		t.Fatalf("issue = %+v, %v", issued, err)
	}
	pastDue, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveMarkPastDue)
	if err != nil || pastDue.Status == nil || *pastDue.Status != entities.InvoicesStatusPastDue {
		t.Fatalf("mark_past_due = %+v, %v", pastDue, err)
	}
	paid, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveMarkPaid)
	if err != nil || paid.Status == nil || *paid.Status != entities.InvoicesStatusPaid || paid.PaidOn == nil {
		t.Fatalf("mark_paid = %+v, %v", paid, err)
	}
}

// The subscription lifecycle: a canceled subscription is terminal, so
// no move runs from it — and `mark_past_due` takes an active one to
// past_due, the dunning loop the advisory block never named.
func TestSubscriptionLifecycleIsEnforced(t *testing.T) {
	ctx, app := lifecycleApp(t)
	f := lifecycleFixturesFrom(ctx)
	subs := entities.NewSubscriptionsRepo(app)
	sub, err := subs.Create(ctx, entities.SubscriptionsInput{CustomerId: f.customer, PlanId: f.plan})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sub.Status == nil || *sub.Status != entities.SubscriptionsStatusTrialing {
		t.Fatalf("a fresh subscription starts trialing: %+v", sub)
	}
	if _, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveMarkPastDue); err == nil {
		t.Fatal("mark_past_due from trialing must be refused: only an active subscription goes past due")
	}
	active, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveActivate)
	if err != nil || active.Status == nil || *active.Status != entities.SubscriptionsStatusActive {
		t.Fatalf("activate = %+v, %v", active, err)
	}
	pastDue, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveMarkPastDue)
	if err != nil || pastDue.Status == nil || *pastDue.Status != entities.SubscriptionsStatusPastDue {
		t.Fatalf("mark_past_due = %+v, %v", pastDue, err)
	}
	recovered, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveActivate)
	if err != nil || recovered.Status == nil || *recovered.Status != entities.SubscriptionsStatusActive {
		t.Fatalf("re-activate = %+v, %v", recovered, err)
	}
	canceled, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveCancel)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Status == nil || *canceled.Status != entities.SubscriptionsStatusCanceled {
		t.Fatalf("cancel = %+v", canceled)
	}
	if _, err := subs.Transition(ctx, sub.ID, entities.SubscriptionsMoveActivate); err == nil {
		t.Fatal("activate from canceled must be refused: canceled is terminal")
	}
}

// Issuing an invoice records the day, the way mark_paid records paid_on:
// every seeded issued invoice carries issued_on, and an invoice a user
// issued showed a blank "Issued On" column forever.
func TestIssueStampsIssuedOn(t *testing.T) {
	ctx, app := lifecycleApp(t)
	f := lifecycleFixturesFrom(ctx)
	invoices := entities.NewInvoicesRepo(app)
	inv, err := invoices.Create(ctx, entities.InvoicesInput{CustomerId: f.customer, Number: "T-3", Amount: "10"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if inv.IssuedOn != nil {
		t.Fatalf("a draft carries issued_on %q before it is issued", *inv.IssuedOn)
	}
	issued, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveIssue)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.IssuedOn == nil || *issued.IssuedOn == "" {
		t.Fatalf("issue left issued_on blank: %+v", issued)
	}
}
