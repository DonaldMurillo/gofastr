package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// savedViewsEnv is an admin with Config.SavedViews on.
type savedViewsEnv struct {
	*env
	store entityui.SavedViewStore
}

func newSavedViewsEnv(t *testing.T) *savedViewsEnv {
	t.Helper()
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}, SavedViews: true}, nil)
	if x.b.SavedViews() == nil {
		t.Fatal("Config.SavedViews left the store nil")
	}
	return &savedViewsEnv{env: x, store: x.b.SavedViews()}
}

// ownerCtx is a request context for user id under tenant, the shape an
// app's auth layer produces. An empty id is a caller with no user.
func ownerCtx(id, ten string) context.Context {
	ctx := context.Background()
	if id != "" {
		ctx = handler.SetUser(ctx, roleUser{id: id})
	}
	if ten != "" {
		ctx = tenant.SetTenantID(ctx, ten)
	}
	return ctx
}

func mustCreateView(t *testing.T, ctx context.Context, s entityui.SavedViewStore, name string) entityui.SavedView {
	t.Helper()
	v, err := s.Create(ctx, entityui.SavedView{Entity: "posts", Name: name, Filter: "status = published"})
	if err != nil {
		t.Fatalf("create view %q: %v", name, err)
	}
	return v
}

// Another owner's view is invisible: List hides it, Get and Delete answer
// ErrSavedViewNotFound, the same as an id that never existed.
func TestSavedViewsOwnerIsolation(t *testing.T) {
	x := newSavedViewsEnv(t)
	mine := mustCreateView(t, ownerCtx("u1", ""), x.store, "Mine")
	other := mustCreateView(t, ownerCtx("u2", ""), x.store, "Theirs")

	u1 := ownerCtx("u1", "")
	views, err := x.store.List(u1, "posts")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.ID == other.ID {
			t.Fatal("SECURITY: u1's list shows u2's view")
		}
	}
	if _, err := x.store.Get(u1, "posts", other.ID); !errors.Is(err, entityui.ErrSavedViewNotFound) {
		t.Fatalf("SECURITY: u1 Get u2's view = %v, want ErrSavedViewNotFound", err)
	}
	if err := x.store.Delete(u1, "posts", other.ID); !errors.Is(err, entityui.ErrSavedViewNotFound) {
		t.Fatalf("SECURITY: u1 Delete u2's view = %v, want ErrSavedViewNotFound", err)
	}
	// u2's view survives u1's attempts.
	if _, err := x.store.Get(ownerCtx("u2", ""), "posts", other.ID); err != nil {
		t.Fatalf("u2's view was disturbed: %v", err)
	}
	// Sanity: the owner still sees their own.
	if _, err := x.store.Get(u1, "posts", mine.ID); err != nil {
		t.Fatalf("u1 cannot read their own view: %v", err)
	}
}

// The same user id under another tenant is another viewer: tenant walls
// the store the same way owner does.
func TestSavedViewsTenantIsolation(t *testing.T) {
	x := newSavedViewsEnv(t)
	home := mustCreateView(t, ownerCtx("u1", "t1"), x.store, "Home")

	peer := ownerCtx("u1", "t2") // same owner id, other tenant
	if views, err := x.store.List(peer, "posts"); err != nil {
		t.Fatal(err)
	} else if len(views) != 0 {
		t.Fatalf("SECURITY: tenant t2 listed t1's %d views", len(views))
	}
	if _, err := x.store.Get(peer, "posts", home.ID); !errors.Is(err, entityui.ErrSavedViewNotFound) {
		t.Fatalf("SECURITY: tenant t2 read t1's view: %v", err)
	}
	if err := x.store.Delete(peer, "posts", home.ID); !errors.Is(err, entityui.ErrSavedViewNotFound) {
		t.Fatalf("SECURITY: tenant t2 deleted t1's view: %v", err)
	}
	if _, err := x.store.Get(ownerCtx("u1", "t1"), "posts", home.ID); err != nil {
		t.Fatalf("t1's view was disturbed: %v", err)
	}
}

// A caller with no user on ctx is refused for every method.
func TestSavedViewsRefuseNoUser(t *testing.T) {
	x := newSavedViewsEnv(t)
	ctx := context.Background()
	if _, err := x.store.List(ctx, "posts"); err == nil {
		t.Error("SECURITY: List ran with no user")
	}
	if _, err := x.store.Get(ctx, "posts", "any"); err == nil {
		t.Error("SECURITY: Get ran with no user")
	}
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "X"}); err == nil {
		t.Error("SECURITY: Create ran with no user")
	}
	if err := x.store.Delete(ctx, "posts", "any"); err == nil {
		t.Error("SECURITY: Delete ran with no user")
	}
}

// The per-owner-per-entity cap holds; the cap check and the insert are
// one transaction, so a name created concurrently cannot slip past it.
func TestSavedViewsCapPerOwnerEntity(t *testing.T) {
	x := newSavedViewsEnv(t)
	ctx := ownerCtx("u1", "")
	for i := range entityui.SavedViewCap {
		mustCreateView(t, ctx, x.store, "view-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	_, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "one too many"})
	if !errors.Is(err, entityui.ErrSavedViewCap) {
		t.Fatalf("over-cap Create = %v, want ErrSavedViewCap", err)
	}
	// The cap is per owner and per entity: another owner still creates,
	// and the same owner creates for another entity.
	if _, err := x.store.Create(ownerCtx("u2", ""), entityui.SavedView{Entity: "posts", Name: "fine"}); err != nil {
		t.Fatalf("another owner was refused: %v", err)
	}
}

// Limits: a blank name, a duplicate name, and over-length name, filter
// and column list are refused.
func TestSavedViewsContractLimits(t *testing.T) {
	x := newSavedViewsEnv(t)
	ctx := ownerCtx("u1", "")
	mustCreateView(t, ctx, x.store, "Mine")

	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "  "}); !errors.Is(err, entityui.ErrSavedViewBlank) {
		t.Fatalf("blank name = %v, want ErrSavedViewBlank", err)
	}
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "Mine"}); !errors.Is(err, entityui.ErrSavedViewExists) {
		t.Fatalf("duplicate name = %v, want ErrSavedViewExists", err)
	}
	// The same name under another entity is fine.
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "notes", Name: "Mine"}); err != nil {
		t.Fatalf("same name on another entity = %v", err)
	}
	tooLongName := strings.Repeat("n", entityui.SavedViewNameMax+1)
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: tooLongName}); !errors.Is(err, entityui.ErrSavedViewTooLong) {
		t.Fatalf("long name = %v, want ErrSavedViewTooLong", err)
	}
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "ok", Filter: strings.Repeat("f", entityui.SavedViewFilterMax+1)}); !errors.Is(err, entityui.ErrSavedViewTooLong) {
		t.Fatalf("long filter = %v, want ErrSavedViewTooLong", err)
	}
	cols := make([]string, entityui.SavedViewColumnsMax+1)
	for i := range cols {
		cols[i] = "c"
	}
	if _, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: "ok", Columns: cols}); !errors.Is(err, entityui.ErrSavedViewTooLong) {
		t.Fatalf("many columns = %v, want ErrSavedViewTooLong", err)
	}
}

// A view's fields round-trip: filter text, columns in order, and names
// sort in List. SQL metacharacters in a name are stored literally.
func TestSavedViewsRoundTrip(t *testing.T) {
	x := newSavedViewsEnv(t)
	ctx := ownerCtx("u1", "")
	odd := "mine'; DROP TABLE admin_saved_views; --"
	v, err := x.store.Create(ctx, entityui.SavedView{Entity: "posts", Name: odd, Filter: "title ~ 'x'", Columns: []string{"status", "title"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := x.store.Get(ctx, "posts", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != odd || got.Filter != "title ~ 'x'" || strings.Join(got.Columns, ",") != "status,title" {
		t.Fatalf("round-trip changed the view: %+v", got)
	}
	var one int
	if err := x.db.QueryRow(`SELECT 1 FROM admin_saved_views WHERE name = ?`, odd).Scan(&one); err != nil {
		t.Fatalf("the odd name was not stored literally: %v", err)
	}
	// List is by name; the odd name sorts beside a plain one.
	mustCreateView(t, ctx, x.store, "aaa")
	views, err := x.store.List(ctx, "posts")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].Name != "aaa" {
		t.Fatalf("List order = %v, want aaa first", views)
	}
	// Delete removes only the caller's view.
	if err := x.store.Delete(ctx, "posts", v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := x.store.Get(ctx, "posts", v.ID); !errors.Is(err, entityui.ErrSavedViewNotFound) {
		t.Fatalf("deleted view = %v, want ErrSavedViewNotFound", err)
	}
	// A no-columns view reads back with no columns.
	plain := mustCreateView(t, ctx, x.store, "plain")
	if got, err := x.store.Get(ctx, "posts", plain.ID); err != nil || got.Columns != nil && len(got.Columns) != 0 {
		t.Fatalf("no-columns round-trip = %+v, %v", got, err)
	}
}
