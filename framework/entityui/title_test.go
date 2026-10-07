package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// subscriptionUI is customers, plans and subscriptions titled by their
// customer and plan, with invoices pointing at a subscription. plans is
// public unless gatedPlans, which leaves it at the default posture an
// anonymous caller may not read.
func subscriptionUI(t *testing.T, gatedPlans bool) *testUI {
	t.Helper()
	public := &entity.ExposureConfig{Public: true}
	plans := entity.EntityConfig{Fields: fields(schema.Field{Name: "name", Type: schema.String}), Exposure: public}
	if gatedPlans {
		plans.Exposure = nil
	}
	return newTestUI(t,
		map[string]entity.EntityConfig{
			"customers": {Fields: fields(schema.Field{Name: "name", Type: schema.String}), Exposure: public},
			"plans":     plans,
			"subscriptions": {
				Fields: fields(
					schema.Field{Name: "customer_id", Type: schema.Relation, To: "customers"},
					schema.Field{Name: "plan_id", Type: schema.Relation, To: "plans"},
					schema.Field{Name: "seats", Type: schema.Int},
				),
				Exposure: public,
				Display:  &entity.DisplayConfig{TitleFields: []string{"customer_id", "plan_id"}},
			},
			"invoices": {
				Fields: fields(
					schema.Field{Name: "number", Type: schema.String},
					schema.Field{Name: "subscription_id", Type: schema.Relation, To: "subscriptions"},
				),
				Exposure: public,
			},
		},
		map[string][]map[string]any{
			"customers": {{"id": "c1", "name": "Ada Lovelace"}, {"id": "c2", "name": "Grace Hopper"}},
			"plans":     {{"id": "p1", "name": "Pro"}, {"id": "p2", "name": "Team"}},
			"subscriptions": {
				{"id": "s1", "customer_id": "c1", "plan_id": "p1", "seats": 3},
				{"id": "s2", "customer_id": "c2", "plan_id": "p2", "seats": 9},
				{"id": "s3", "customer_id": "c1", "plan_id": "p2", "seats": 1},
			},
			"invoices": {{"id": "i1", "number": "INV-1", "subscription_id": "s1"}},
		},
		withAPI(map[string]string{"customers": "/api/customers", "plans": "/api/plans", "subscriptions": "/api/subscriptions", "invoices": "/api/invoices"}),
	)
}

// A record titled by two relations reads as the related records' own
// titles joined: the subscription is "Ada Lovelace · Pro", live and from
// a stored copy, never its ids.
func TestTitleJoinsRelationTitles(t *testing.T) {
	x := subscriptionUI(t, false)
	ctx := x.ctx("/subscriptions/s1", "")
	if got, ok := x.ui.RecordTitle(ctx, "subscriptions", "s1"); !ok || got != "Ada Lovelace · Pro" {
		t.Fatalf("RecordTitle = %q, %v; want Ada Lovelace · Pro", got, ok)
	}
	snap := map[string]any{"id": "s9", "customer_id": "c2", "plan_id": "p1"}
	if got, _ := x.ui.SnapshotTitle(ctx, "subscriptions", snap); got != "Grace Hopper · Pro" {
		t.Fatalf("SnapshotTitle = %q, want Grace Hopper · Pro", got)
	}
	h := string(x.ui.Record("subscriptions", "s1").RenderCtx(ctx))
	if !strings.Contains(h, "Ada Lovelace · Pro</h1>") {
		t.Fatalf("record heading not the composite title:\n%s", h)
	}
}

// A relation part the caller may not read is left out, never shown as
// its id: an anonymous caller sees "Ada Lovelace", not "Ada Lovelace · p1".
func TestTitleDropsRefusedPart(t *testing.T) {
	x := subscriptionUI(t, true)
	got, ok := x.ui.RecordTitle(x.ctx("/subscriptions/s1", ""), "subscriptions", "s1")
	if !ok || got != "Ada Lovelace" {
		t.Fatalf("RecordTitle = %q, %v; want Ada Lovelace", got, ok)
	}
}

// One page names its rows with one read per relation part, not one per
// row: the row menus and checkboxes reuse it. A relation cell pointing
// at a subscription shows its composite title too.
func TestTitlePageReadsOncePerPart(t *testing.T) {
	x := subscriptionUI(t, false)
	ch, err := x.host.Crud(mustEntity(t, x, "plans"))
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(context.Context, any) error { reads++; return nil })

	h := listHTML(t, x.ui.List("subscriptions").Bulk(), x.ctx("/subscriptions", ""))
	for _, want := range []string{"Actions for Ada Lovelace · Pro", "Actions for Grace Hopper · Team", "Select Ada Lovelace · Team"} {
		if !strings.Contains(h, want) {
			t.Errorf("list missing %q:\n%s", want, h)
		}
	}
	// One read for the plan column's cells, one for the titles.
	if reads != 2 {
		t.Errorf("plans read %d times for one page, want 2", reads)
	}

	inv := listHTML(t, x.ui.List("invoices"), x.ctx("/invoices", ""))
	if !strings.Contains(inv, "Ada Lovelace · Pro") {
		t.Errorf("subscription cell not the composite title:\n%s", inv)
	}
}

// A relation part is followed one hop: entities titled by each other do
// not recurse, and the second hop's relation is left out.
func TestTitleFollowsOneHop(t *testing.T) {
	public := &entity.ExposureConfig{Public: true}
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"teams": {Fields: fields(
				schema.Field{Name: "lead_id", Type: schema.Relation, To: "people"},
				schema.Field{Name: "code", Type: schema.String},
			), Exposure: public, Display: &entity.DisplayConfig{TitleFields: []string{"lead_id", "code"}}},
			"people": {Fields: fields(
				schema.Field{Name: "team_id", Type: schema.Relation, To: "teams"},
				schema.Field{Name: "name", Type: schema.String},
			), Exposure: public, Display: &entity.DisplayConfig{TitleFields: []string{"name", "team_id"}}},
		},
		map[string][]map[string]any{
			"teams":  {{"id": "t1", "code": "CORE"}},
			"people": {{"id": "h1", "name": "Hedy"}},
		},
		withAPI(map[string]string{"teams": "/api/teams", "people": "/api/people"}),
	)
	// Each points at the other, set after both rows exist.
	for _, q := range []string{`UPDATE teams SET lead_id = 'h1' WHERE id = 't1'`, `UPDATE people SET team_id = 't1' WHERE id = 'h1'`} {
		if _, err := x.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := x.ctx("/teams/t1", "")
	if got, _ := x.ui.RecordTitle(ctx, "teams", "t1"); got != "Hedy · CORE" {
		t.Fatalf("team title = %q, want Hedy · CORE (the lead's team left out)", got)
	}
	if got, _ := x.ui.RecordTitle(ctx, "people", "h1"); got != "Hedy · CORE" {
		t.Fatalf("person title = %q, want Hedy · CORE (the team's lead left out)", got)
	}
}
