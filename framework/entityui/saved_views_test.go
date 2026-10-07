package entityui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Saved views: the caller's named filter-and-columns state, opened
// through ?saved=, saved and deleted through the handler the host
// mounts at <write base>/_views.

// memSavedViews is an in-memory SavedViewStore scoped by the signed-in
// user: a view another user made answers ErrSavedViewNotFound, exactly
// like a missing id. It enforces the interface's limits.
type memSavedViews struct {
	mu    sync.Mutex
	next  int
	views map[string]memSavedView
}

type memSavedView struct {
	owner string
	v     SavedView
}

func newMemSavedViews() *memSavedViews {
	return &memSavedViews{views: map[string]memSavedView{}}
}

func ownerOf(ctx context.Context) (string, error) {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return "", errors.New("no user")
	}
	id, ok := u.(interface{ GetID() string })
	if !ok {
		return "", errors.New("no user id")
	}
	return id.GetID(), nil
}

func (m *memSavedViews) List(ctx context.Context, entityName string) ([]SavedView, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SavedView
	for _, mv := range m.views {
		if mv.owner == owner && mv.v.Entity == entityName {
			out = append(out, mv.v)
		}
	}
	// By name, the contract's order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (m *memSavedViews) Get(ctx context.Context, entityName, id string) (SavedView, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return SavedView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mv, ok := m.views[id]
	if !ok || mv.owner != owner || mv.v.Entity != entityName {
		return SavedView{}, ErrSavedViewNotFound
	}
	return mv.v, nil
}

func (m *memSavedViews) Create(ctx context.Context, v SavedView) (SavedView, error) {
	owner, err := ownerOf(ctx)
	if err != nil {
		return SavedView{}, err
	}
	if strings.TrimSpace(v.Name) == "" {
		return SavedView{}, ErrSavedViewBlank
	}
	if len(v.Name) > SavedViewNameMax || len(v.Filter) > SavedViewFilterMax || len(v.Columns) > SavedViewColumnsMax {
		return SavedView{}, ErrSavedViewTooLong
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, mv := range m.views {
		if mv.owner == owner && mv.v.Entity == v.Entity {
			n++
			if mv.v.Name == v.Name {
				return SavedView{}, ErrSavedViewExists
			}
		}
	}
	if n >= SavedViewCap {
		return SavedView{}, ErrSavedViewCap
	}
	m.next++
	v.ID = fmt.Sprintf("sv-%d", m.next)
	m.views[v.ID] = memSavedView{owner: owner, v: v}
	return v, nil
}

func (m *memSavedViews) Delete(ctx context.Context, entityName, id string) error {
	owner, err := ownerOf(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mv, ok := m.views[id]
	if !ok || mv.owner != owner || mv.v.Entity != entityName {
		return ErrSavedViewNotFound
	}
	delete(m.views, id)
	return nil
}

// put seeds one view directly, bypassing Create's checks.
func (m *memSavedViews) put(owner string, v SavedView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	if v.ID == "" {
		v.ID = fmt.Sprintf("sv-%d", m.next)
	}
	m.views[v.ID] = memSavedView{owner: owner, v: v}
}

// savedUI is an orders app with saved views on.
func savedUI(t *testing.T) (*testUI, *memSavedViews) {
	t.Helper()
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	store := newMemSavedViews()
	installOwnerExtractor(t)
	x.ui = x.ui.WithSavedViews(store)
	return x, store
}

func TestSavedViewOpensAndNarrows(t *testing.T) {
	x, store := savedUI(t)
	// u1 saves "Open only" carrying the filter text.
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "Open only", Filter: `status = "open"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved="+saved.ID, "u1"))
	if strings.Contains(html, "zeta") {
		t.Errorf("the saved view's filter did not narrow the list:\n%s", html)
	}
	if !strings.Contains(html, "alpha") {
		t.Errorf("the saved view dropped the matching row:\n%s", html)
	}
	// The strip carries the view, marked current, and the chips show
	// the filter — one source of truth.
	for _, want := range []string{"Open only", `aria-current="true"`, `status = &quot;open&quot;`} {
		if !strings.Contains(html, want) {
			t.Errorf("the saved strip is missing %q:\n%s", want, html)
		}
	}
	// The save form is there too, carrying the filter it would save.
	if !strings.Contains(html, `action="/api/orders/_views"`) {
		t.Errorf("the save form does not post to the write base:\n%s", html)
	}
}

func TestSavedViewColumnsApply(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "Two cols", Columns: []string{"status", "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved="+saved.ID, "u1"))
	if colOrder(html, "Status", "Name") < 0 {
		t.Errorf("the saved view's columns did not apply:\n%s", html)
	}
	if colOrder(html, "Amount") >= 0 {
		t.Errorf("the saved view's hidden column still shows:\n%s", html)
	}
}

func TestSavedViewExplicitParamsWin(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "Open only", Filter: `status = "open"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := listHTML(t, x.ui.List("orders").SavedViews(),
		x.userCtx("/orders", "?saved="+saved.ID+"&filter=status+%3D+%22paid%22", "u1"))
	if strings.Contains(html, "alpha") || !strings.Contains(html, "zeta") {
		t.Errorf("an explicit filter did not win over the saved one:\n%s", html)
	}
}

func TestSavedViewUnknownIDShowsCallout(t *testing.T) {
	x, _ := savedUI(t)
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved=nope", "u1"))
	if !strings.Contains(html, "This view no longer applies") {
		t.Errorf("an unknown saved id drew no callout:\n%s", html)
	}
	if !strings.Contains(html, "alpha") || !strings.Contains(html, "zeta") {
		t.Errorf("the unknown view changed the rows:\n%s", html)
	}
}

func TestSavedViewStaleFilterShowsCallout(t *testing.T) {
	x, store := savedUI(t)
	// A view that names a field the entity does not have (or a now
	// Hidden one): the store was bypassed, the open path still refuses.
	store.put("u1", SavedView{Entity: "orders", Name: "Stale", Filter: "nosuchfield = 1"})
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved=sv-1", "u1"))
	if !strings.Contains(html, "This view no longer applies") {
		t.Errorf("a stale saved filter drew no callout:\n%s", html)
	}
	if !strings.Contains(html, "alpha") || !strings.Contains(html, "zeta") {
		t.Errorf("the stale view still narrowed the rows:\n%s", html)
	}
	// Its name is not leaked as the open one.
	if strings.Contains(html, `aria-current="true"`) {
		t.Errorf("the stale view is marked current:\n%s", html)
	}
}

func TestSavedViewBadColumnShowsCallout(t *testing.T) {
	x, store := savedUI(t)
	store.put("u1", SavedView{Entity: "orders", Name: "Bad cols", Columns: []string{"name", "nosuchfield"}})
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved=sv-1", "u1"))
	if !strings.Contains(html, "This view no longer applies") {
		t.Errorf("a stale column list drew no callout:\n%s", html)
	}
	if colOrder(html, "Name", "Status", "Amount") < 0 {
		t.Errorf("the stale columns changed the list:\n%s", html)
	}
}

func TestSavedViewsNoopWithoutStore(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	installOwnerExtractor(t)
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved=any", "u1"))
	if strings.Contains(html, "Save view") {
		t.Errorf("saved views drew without a store:\n%s", html)
	}
	if strings.Contains(html, "This view no longer applies") {
		t.Errorf("saved views without a store answered the param:\n%s", html)
	}
}

// savedViewsMux mounts the handler the way the host does.
func savedViewsMux(x *testUI) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/orders/_views", x.ui.SavedViewsHandler("orders"))
	mux.Handle("POST /api/orders/_views/_delete/{id}", x.ui.SavedViewsHandler("orders"))
	return mux
}

// postSavedViews posts to the mounted handler as user.
func postSavedViews(t *testing.T, x *testUI, user, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, user)
	w := httptest.NewRecorder()
	savedViewsMux(x).ServeHTTP(w, r)
	return w
}

func TestSaveViewStoresAndRedirects(t *testing.T) {
	x, store := savedUI(t)
	w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{
		"name": {"Open only"}, "filter": {`status = "open"`}, "cols": {"name,status"},
		"back": {"/orders"}, "key": {""},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save answered %d (%s), want 303", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/orders?saved=") {
		t.Fatalf("save Location = %q, want the list with the view open", loc)
	}
	views, err := store.List(asUser(context.Background(), "u1"), "orders")
	if err != nil || len(views) != 1 {
		t.Fatalf("the stored views = %+v (%v)", views, err)
	}
	if views[0].Filter != `status = "open"` || strings.Join(views[0].Columns, ",") != "name,status" {
		t.Errorf("the stored view = %+v", views[0])
	}
	// Following the redirect opens the saved view.
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?"+strings.TrimPrefix(loc, "/orders?"), "u1"))
	if strings.Contains(html, "zeta") || !strings.Contains(html, `aria-current="true"`) {
		t.Errorf("the redirect did not open the saved view:\n%s", html)
	}
}

func TestSaveViewRefusesInvalidFilter(t *testing.T) {
	x, store := savedUI(t)
	w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{
		"name": {"Bad"}, "filter": {"status ="}, "back": {"/orders"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an invalid filter answered %d, want 400", w.Code)
	}
	if views, _ := store.List(asUser(context.Background(), "u1"), "orders"); len(views) != 0 {
		t.Errorf("an invalid filter was stored: %+v", views)
	}
}

func TestSaveViewRefusesInvalidColumns(t *testing.T) {
	x, store := savedUI(t)
	w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{
		"name": {"Bad cols"}, "cols": {"name,nosuchfield"}, "back": {"/orders"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an invalid column list answered %d, want 400", w.Code)
	}
	if views, _ := store.List(asUser(context.Background(), "u1"), "orders"); len(views) != 0 {
		t.Errorf("an invalid column list was stored: %+v", views)
	}
}

func TestSaveViewMapsStoreRefusals(t *testing.T) {
	x, _ := savedUI(t)
	base := url.Values{"name": {"Mine"}, "filter": {`status = "open"`}, "back": {"/orders"}}
	if w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{"name": {"  "}, "back": {"/orders"}}); w.Code != http.StatusBadRequest {
		t.Errorf("a blank name answered %d, want 400", w.Code)
	}
	if w := postSavedViews(t, x, "u1", "/api/orders/_views", base); w.Code != http.StatusSeeOther {
		t.Fatalf("setup save failed: %d", w.Code)
	}
	dupe := url.Values{}
	for k, v := range base {
		dupe[k] = v
	}
	if w := postSavedViews(t, x, "u1", "/api/orders/_views", dupe); w.Code != http.StatusConflict {
		t.Errorf("a duplicate name answered %d, want 409", w.Code)
	}
	long := url.Values{"name": {strings.Repeat("x", SavedViewNameMax+1)}, "back": {"/orders"}}
	if w := postSavedViews(t, x, "u1", "/api/orders/_views", long); w.Code != http.StatusBadRequest {
		t.Errorf("an over-long name answered %d, want 400", w.Code)
	}
}

func TestDeleteViewRemovesAndRedirects(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{Entity: "orders", Name: "Mine", Filter: `status = "open"`})
	if err != nil {
		t.Fatal(err)
	}
	w := postSavedViews(t, x, "u1", "/api/orders/_views/_delete/"+saved.ID, url.Values{
		"back": {"/orders?saved=" + saved.ID},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("delete answered %d (%s), want 303", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/orders" {
		t.Errorf("delete Location = %q, want the list with no view open", got)
	}
	if _, err := store.Get(asUser(context.Background(), "u1"), "orders", saved.ID); !errors.Is(err, ErrSavedViewNotFound) {
		t.Errorf("the view survived its delete: %v", err)
	}
	// A second delete of the same id answers 404.
	if w := postSavedViews(t, x, "u1", "/api/orders/_views/_delete/"+saved.ID, url.Values{"back": {"/orders"}}); w.Code != http.StatusNotFound {
		t.Errorf("deleting a gone view answered %d, want 404", w.Code)
	}
}

func TestSavedViewJSONAnswer(t *testing.T) {
	x, store := savedUI(t)
	r := httptest.NewRequest(http.MethodPost, "/api/orders/_views", strings.NewReader(`{"name":"Open only","filter":"status = \"open\""}`))
	r.Header.Set("Content-Type", "application/json")
	r = withUserRequest(r, "u1")
	w := httptest.NewRecorder()
	savedViewsMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("JSON save answered %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"sv-`) {
		t.Errorf("JSON save body = %s", w.Body.String())
	}
	if views, _ := store.List(asUser(context.Background(), "u1"), "orders"); len(views) != 1 {
		t.Errorf("the JSON save did not store: %+v", views)
	}
}

func TestSaveViewBodyCapped(t *testing.T) {
	x, store := savedUI(t)
	big := strings.Repeat("a", 64<<10)
	w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{
		"name": {"Mine"}, "filter": {`status = "open"`}, "back": {"/orders?x=" + big},
	})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized body answered %d, want 413", w.Code)
	}
	if views, _ := store.List(asUser(context.Background(), "u1"), "orders"); len(views) != 0 {
		t.Errorf("an oversized body still saved a view: %+v", views)
	}
}
