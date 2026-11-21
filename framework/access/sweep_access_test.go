package access

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Replica B revokes a code-seeded grant, then the tombstone vanishes with no grant row left (a late re-grant's lift): both replicas must agree.
func TestSweepAccessRevokeLeavesBaselineAlone(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	a, policyA := seedStore(t, db, "editor", "posts:read")
	b, policyB := seedStore(t, db, "editor", "posts:read")
	if err := b.Revoke(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM access_grants_revoked`); err != nil {
		t.Fatalf("delete tombstone: %v", err)
	}
	for _, s := range []*GrantStore{a, b} {
		if err := s.reloadRole(ctx, "editor"); err != nil {
			t.Fatalf("reloadRole: %v", err)
		}
	}
	gotA := permIn(policyA.PermissionsOf("editor"), "posts:read")
	gotB := permIn(policyB.PermissionsOf("editor"), "posts:read")
	if gotA != gotB {
		t.Fatalf("replicas disagree from the same shared rows: A=%v B=%v", gotA, gotB)
	}
}

func TestSweepAccessTransitionsSerialize(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	store, policy := seedStore(t, db, "editor", "posts:read")
	steps := map[string]func(){
		"reloadRole":     func() { _ = store.reloadRole(ctx, "editor") },
		"reloadRole all": func() { _ = store.reloadRole(ctx, "") },
		"Grant":          func() { _ = store.Grant(ctx, "editor", "posts:write") },
		"Revoke":         func() { _ = store.Revoke(ctx, "editor", "posts:write") },
		"LoadInto":       func() { _ = store.LoadInto(ctx, policy) },
	}
	for name, step := range steps {
		store.transitionMu.Lock()
		done := make(chan struct{})
		go func() {
			step()
			close(done)
		}()
		select {
		case <-done:
			store.transitionMu.Unlock()
			t.Fatalf("%s ran while another transition held transitionMu", name)
		case <-time.After(100 * time.Millisecond):
		}
		store.transitionMu.Unlock()
		<-done
	}
}

func TestSweepAccessExportsBothTables(t *testing.T) {
	db := openAccessDB(t)
	store := NewGrantStore(db, NewRolePolicy(), WithGrantTable("sweep_grants"))
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	found := map[string]bool{}
	for _, e := range datexport.All() {
		found[e.Table] = true
	}
	for _, table := range []string{"sweep_grants", "sweep_grants_revoked"} {
		if !found[table] {
			t.Errorf("table %q is not registered for data export", table)
		}
	}
}

type sweepFailingFanout struct{ published int }

func (f *sweepFailingFanout) Publish(context.Context, string, []byte) error {
	f.published++
	return nil
}

func (f *sweepFailingFanout) Subscribe(string, func([]byte)) (func(), error) {
	return nil, errors.New("subscribe refused")
}

func TestSweepAccessSubscribeFailureDisarmsPublish(t *testing.T) {
	db := openAccessDB(t)
	store, _ := seedStore(t, db, "editor")
	f := &sweepFailingFanout{}
	if _, err := store.SetFanout(f); err == nil {
		t.Fatal("SetFanout succeeded with a failing Subscribe")
	}
	if err := store.Grant(context.Background(), "editor", "posts:read"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	store.fanoutMu.Lock()
	attachment := store.attachment
	store.fanoutMu.Unlock()
	if attachment != nil || f.published != 0 {
		t.Fatalf("a store whose Subscribe failed still publishes (attached=%v, published=%d)", attachment != nil, f.published)
	}
}

func TestSweepAccessRemoteSignalDoesNotReloadInline(t *testing.T) {
	db := openAccessDB(t)
	store, _ := seedStore(t, db, "editor", "posts:read")
	stop, err := store.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	defer stop()
	payload, _ := json.Marshal(accessInvalidateMsg{Role: "editor"})
	store.transitionMu.Lock()
	done := make(chan struct{})
	go func() {
		store.attachment.handleRemote(fanout.Wrap("other-node", payload))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		store.transitionMu.Unlock()
		t.Fatal("handleRemote blocked on a reload; the delivery goroutine must only mark the role dirty")
	}
	store.transitionMu.Unlock()
}

type sweepStalledFanout struct{ release chan struct{} }

func (f *sweepStalledFanout) Publish(ctx context.Context, _ string, _ []byte) error {
	select {
	case <-f.release:
	case <-ctx.Done():
	}
	return nil
}

func (f *sweepStalledFanout) Subscribe(string, func([]byte)) (func(), error) {
	return func() {}, nil
}

func TestSweepAccessStalledBusDoesNotBlockGrant(t *testing.T) {
	db := openAccessDB(t)
	store, _ := seedStore(t, db, "editor")
	f := &sweepStalledFanout{release: make(chan struct{})}
	stop, err := store.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	defer stop()
	defer close(f.release)
	done := make(chan error, 2)
	go func() {
		done <- store.Grant(context.Background(), "editor", "posts:read")
		done <- store.Revoke(context.Background(), "editor", "posts:read")
	}()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("write: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Grant or Revoke blocked on a stalled fanout backend")
		}
	}
}

func TestSweepAccessGrantTableMustBeSafe(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithGrantTable accepted an unsafe identifier")
		}
	}()
	NewGrantStore(nil, NewRolePolicy(), WithGrantTable(`grants; DROP TABLE users`))
}

func TestSweepAccessMiddlewareRolesFollowRecheck(t *testing.T) {
	current := []string{"editor"}
	var held context.Context
	h := Middleware(NewRolePolicy(), func(context.Context) []string { return current })(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		held = r.Context()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	current = nil
	refreshed, ok := handler.RecheckPrincipal(held)
	if !ok {
		t.Fatal("RecheckPrincipal refused")
	}
	if got := GetRoles(refreshed); len(got) != 0 {
		t.Fatalf("a dropped role survived the recheck: %v", got)
	}
}

func TestSweepAccessNonStringAuthTenantSharesEntry(t *testing.T) {
	calls := 0
	resolver := NewCachedResolver(func(context.Context) []string {
		calls++
		return []string{"viewer"}
	})
	for range 2 {
		ctx := handler.SetTenant(handler.SetUser(context.Background(), secResolverUser("alice")), new(int))
		resolver.Resolve(ctx)
	}
	if calls != 1 {
		t.Fatalf("a non-string tenant split the cache: %d lookups for one user", calls)
	}
}

// A replica that booted while a wildcard tombstone stood must, once a rolling-deploy peer lifts it, agree with one that booted before.
func TestSweepAccessBootUnderTombstoneKeepsBaseline(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	boot := func(caps ...Permission) (*GrantStore, *RolePolicy) {
		t.Helper()
		policy := NewRolePolicy()
		policy.Register(caps...)
		if slices.Contains(caps, "posts:write") {
			if err := policy.Grant("editor", "posts:write"); err != nil {
				t.Fatalf("code grant: %v", err)
			}
		}
		store := NewGrantStore(db, policy)
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		if err := store.LoadInto(ctx, policy); err != nil {
			t.Fatalf("LoadInto: %v", err)
		}
		return store, policy
	}
	old, _ := boot("posts:read")
	early, policyEarly := boot("posts:read", "posts:write")
	if err := old.Revoke(ctx, "editor", "posts:*"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	late, policyLate := boot("posts:read", "posts:write")
	if permIn(policyLate.PermissionsOf("editor"), "posts:write") {
		t.Fatal("a boot under the tombstone allowed posts:write")
	}
	if err := old.Grant(ctx, "editor", "posts:*"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	for _, s := range []*GrantStore{early, late} {
		if err := s.reloadRole(ctx, "editor"); err != nil {
			t.Fatalf("reloadRole: %v", err)
		}
	}
	var stones int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_grants_revoked`).Scan(&stones); err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	if stones != 0 {
		t.Fatalf("the re-grant left %d tombstones", stones)
	}
	_, policyFresh := boot("posts:read", "posts:write")
	want := permIn(policyFresh.PermissionsOf("editor"), "posts:write")
	gotEarly := permIn(policyEarly.PermissionsOf("editor"), "posts:write")
	gotLate := permIn(policyLate.PermissionsOf("editor"), "posts:write")
	if !want || gotEarly != want || gotLate != want {
		t.Fatalf("code grant with no tombstone: fresh=%v early=%v late=%v, want all true", want, gotEarly, gotLate)
	}
}
