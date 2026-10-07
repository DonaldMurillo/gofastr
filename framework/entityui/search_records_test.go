package entityui

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func searchUI(t *testing.T, cfg entity.EntityConfig) *testUI {
	t.Helper()
	return newTestUI(t,
		map[string]entity.EntityConfig{"orders": cfg},
		map[string][]map[string]any{"orders": ordersRows()},
	)
}

func TestSearchRecordsFindsByTitle(t *testing.T) {
	x := searchUI(t, ordersConfig())
	got := x.ui.SearchRecords(context.Background(), "orders", "alp", 5)
	if len(got) != 1 || got[0].ID != "o1" || got[0].Title != "alpha" {
		t.Fatalf("SearchRecords = %+v; want o1 alpha", got)
	}
	for _, q := range []string{"", "   "} {
		if got := x.ui.SearchRecords(context.Background(), "orders", q, 5); len(got) != 0 {
			t.Errorf("q %q matched %+v", q, got)
		}
	}
	if got := x.ui.SearchRecords(context.Background(), "nope", "alp", 5); len(got) != 0 {
		t.Errorf("an unknown entity matched %+v", got)
	}
}

// An entity without SearchFields is not searchable: ?q= is ignored on
// its list, so a search finds nothing rather than every row.
func TestSearchRecordsNeedsSearchFields(t *testing.T) {
	cfg := ordersConfig()
	cfg.SearchFields = nil
	x := searchUI(t, cfg)
	if got := x.ui.SearchRecords(context.Background(), "orders", "alp", 5); len(got) != 0 {
		t.Fatalf("an entity without SearchFields matched %+v", got)
	}
}

func TestSearchRecordsPassesReadGates(t *testing.T) {
	cfg := ordersConfig()
	cfg.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}}
	x := searchUI(t, cfg)
	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("orders:read")); err != nil {
		t.Fatal(err)
	}
	signedIn := access.WithPolicy(asUser(context.Background(), "u1"), policy)
	if got := x.ui.SearchRecords(signedIn, "orders", "a", 5); len(got) != 0 {
		t.Fatalf("a caller without orders:read matched %+v", got)
	}
	reader := access.WithRoles(signedIn, []string{"reader"})
	if got := x.ui.SearchRecords(reader, "orders", "a", 5); len(got) != 2 {
		t.Fatalf("a reader matched %+v; want both rows", got)
	}
	denied := access.WithDecider(reader, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "o1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	got := x.ui.SearchRecords(denied, "orders", "a", 5)
	if len(got) != 1 || got[0].ID != "o2" {
		t.Fatalf("with o1 denied, matched %+v; want o2 alone", got)
	}
}

func TestSearchRecordsCapsMatches(t *testing.T) {
	x := searchUI(t, ordersConfig())
	if got := x.ui.SearchRecords(context.Background(), "orders", "a", 1); len(got) != 1 {
		t.Fatalf("limit 1 matched %d", len(got))
	}
	if got := x.ui.SearchRecords(context.Background(), "orders", "a", 0); len(got) != 1 {
		t.Fatalf("limit 0 matched %d; want the floor of 1", len(got))
	}
}
