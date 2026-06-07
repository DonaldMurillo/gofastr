package framework

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// --- Access Control Tests ---

func TestRolePolicyGrantAndCheck(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Grant("admin", "posts:read", "posts:write")
	policy.Grant("viewer", "posts:read")

	ctx := context.Background()
	ctx = access.WithPolicy(ctx, policy)

	// Admin can read and write
	ctx = access.WithRoles(ctx, []string{"admin"})
	if !policy.Can(ctx, "posts:read") {
		t.Error("admin should be able to read posts")
	}
	if !policy.Can(ctx, "posts:write") {
		t.Error("admin should be able to write posts")
	}

	// Viewer can only read
	ctx = access.WithRoles(ctx, []string{"viewer"})
	if !policy.Can(ctx, "posts:read") {
		t.Error("viewer should be able to read posts")
	}
	if policy.Can(ctx, "posts:write") {
		t.Error("viewer should NOT be able to write posts")
	}
}

func TestRolePolicyMultipleRolesUnion(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Grant("editor", "posts:read", "posts:write")
	policy.Grant("moderator", "posts:delete", "posts:moderate")

	ctx := context.Background()
	ctx = access.WithPolicy(ctx, policy)
	ctx = access.WithRoles(ctx, []string{"editor", "moderator"})

	// Should have permissions from both roles
	if !policy.Can(ctx, "posts:read") {
		t.Error("should have posts:read from editor role")
	}
	if !policy.Can(ctx, "posts:write") {
		t.Error("should have posts:write from editor role")
	}
	if !policy.Can(ctx, "posts:delete") {
		t.Error("should have posts:delete from moderator role")
	}
	if !policy.Can(ctx, "posts:moderate") {
		t.Error("should have posts:moderate from moderator role")
	}
}

func TestRolePolicyRevoke(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Grant("admin", "posts:read", "posts:write", "posts:delete")

	ctx := context.Background()
	ctx = access.WithPolicy(ctx, policy)
	ctx = access.WithRoles(ctx, []string{"admin"})

	// Confirm all granted
	if !policy.Can(ctx, "posts:read") || !policy.Can(ctx, "posts:write") || !policy.Can(ctx, "posts:delete") {
		t.Fatal("all permissions should be granted initially")
	}

	// Revoke write
	policy.Revoke("admin", "posts:write")

	// Build fresh context (policy is a pointer so changes are reflected)
	if !policy.Can(ctx, "posts:read") {
		t.Error("should still have posts:read")
	}
	if policy.Can(ctx, "posts:write") {
		t.Error("posts:write should have been revoked")
	}
	if !policy.Can(ctx, "posts:delete") {
		t.Error("should still have posts:delete")
	}
}

func TestRolePolicyNoRoles(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Grant("admin", "posts:read")

	ctx := context.Background()
	ctx = access.WithPolicy(ctx, policy)
	// No roles set

	if policy.Can(ctx, "posts:read") {
		t.Error("should deny when no roles are set")
	}
}

// --- Event System Tests ---

func TestEventSubscribeAndEmit(t *testing.T) {
	bus := event.NewEventBus()
	var received event.Event

	bus.On("user.created", func(ctx context.Context, event event.Event) error {
		received = event
		return nil
	})

	ctx := context.Background()
	evt := event.Event{Type: "user.created", Data: map[string]any{"id": 42}}

	if err := bus.Emit(ctx, evt); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}

	if received.Type != "user.created" {
		t.Errorf("expected type user.created, got %s", received.Type)
	}
	if received.Timestamp.IsZero() {
		t.Error("timestamp should have been set")
	}
}

func TestEventEmitCallsAllHandlersInOrder(t *testing.T) {
	bus := event.NewEventBus()
	var order []int

	bus.On("test.order", func(ctx context.Context, event event.Event) error {
		order = append(order, 1)
		return nil
	})
	bus.On("test.order", func(ctx context.Context, event event.Event) error {
		order = append(order, 2)
		return nil
	})
	bus.On("test.order", func(ctx context.Context, event event.Event) error {
		order = append(order, 3)
		return nil
	})

	ctx := context.Background()
	if err := bus.Emit(ctx, event.Event{Type: "test.order"}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 handler calls, got %d", len(order))
	}
	for i, expected := range []int{1, 2, 3} {
		if order[i] != expected {
			t.Errorf("handler %d: expected %d, got %d", i, expected, order[i])
		}
	}
}

func TestEventEmitStopsOnError(t *testing.T) {
	bus := event.NewEventBus()
	var called bool

	bus.On("test.err", func(ctx context.Context, event event.Event) error {
		return errors.New("boom")
	})
	bus.On("test.err", func(ctx context.Context, event event.Event) error {
		called = true
		return nil
	})

	ctx := context.Background()
	err := bus.Emit(ctx, event.Event{Type: "test.err"})
	if err == nil {
		t.Fatal("expected error from Emit")
	}
	if called {
		t.Error("second handler should not have been called after first error")
	}
}

// --- Pagination Tests ---

func TestCursorEncodeDecodeRoundTrip(t *testing.T) {
	field, value := "id", "abc123"
	encoded := pagination.EncodeCursor(field, value)

	decodedField, decodedValue, err := pagination.DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if decodedField != field {
		t.Errorf("field: expected %q, got %q", field, decodedField)
	}
	if decodedValue != value {
		t.Errorf("value: expected %q, got %q", value, decodedValue)
	}
}

func TestCursorEncodeDecodeIntValue(t *testing.T) {
	encoded := pagination.EncodeCursor("created_at", 12345)

	field, value, err := pagination.DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor error: %v", err)
	}
	if field != "created_at" {
		t.Errorf("field: expected %q, got %q", "created_at", field)
	}
	if value != "12345" {
		t.Errorf("value: expected %q, got %q", "12345", value)
	}
}

func TestCursorPageWithMore(t *testing.T) {
	data := []map[string]any{
		{"id": 1, "name": "a"},
		{"id": 2, "name": "b"},
		{"id": 3, "name": "c"},
		{"id": 4, "name": "d"},
	}
	// Request limit=3, so we get 4 rows back (3+1 for hasMore check)
	page := pagination.NewCursorPage(data, "id", 3)

	if !page.HasMore {
		t.Error("expected HasMore=true")
	}
	if len(page.Data) != 3 {
		t.Errorf("expected 3 data items, got %d", len(page.Data))
	}
	if page.Cursor == "" {
		t.Error("expected a next cursor")
	}
	// Cursor should encode the last item's id (3)
	field, val, _ := pagination.DecodeCursor(page.Cursor)
	if field != "id" {
		t.Errorf("cursor field: expected id, got %s", field)
	}
	if val != "3" {
		t.Errorf("cursor value: expected 3, got %s", val)
	}
}

func TestCursorPageNoMore(t *testing.T) {
	data := []map[string]any{
		{"id": 1},
		{"id": 2},
	}
	// Only 2 rows, limit=3 → no more
	page := pagination.NewCursorPage(data, "id", 3)

	if page.HasMore {
		t.Error("expected HasMore=false")
	}
	if page.Cursor != "" {
		t.Error("cursor should be empty when no more results")
	}
}

func TestOffsetPageCalculation(t *testing.T) {
	data := []map[string]any{
		{"id": 1},
		{"id": 2},
	}
	page := pagination.NewOffsetPage(data, 2, 10, 55)

	if page.Page != 2 {
		t.Errorf("page: expected 2, got %d", page.Page)
	}
	if page.PageSize != 10 {
		t.Errorf("page_size: expected 10, got %d", page.PageSize)
	}
	if page.Total != 55 {
		t.Errorf("total: expected 55, got %d", page.Total)
	}
	if page.TotalPages != 6 {
		t.Errorf("total_pages: expected 6 (ceil(55/10)), got %d", page.TotalPages)
	}
}

func TestOffsetPageExactFit(t *testing.T) {
	data := []map[string]any{{"id": 1}}
	page := pagination.NewOffsetPage(data, 1, 10, 20)

	if page.TotalPages != 2 {
		t.Errorf("total_pages: expected 2, got %d", page.TotalPages)
	}
}

func TestOffsetPageZeroTotal(t *testing.T) {
	page := pagination.NewOffsetPage(nil, 1, 10, 0)

	if page.TotalPages != 0 {
		t.Errorf("total_pages: expected 0, got %d", page.TotalPages)
	}
}
