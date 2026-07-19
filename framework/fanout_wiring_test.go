package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// TestWithFanout_CrossAppDelivery drives the full seam: a CRUD write on app
// A emits on A's bus, the bridge mirrors it through the shared fanout, and a
// plain bus subscriber on app B (a second "replica") receives it — marked
// remote.
func TestWithFanout_CrossAppDelivery(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		f := fanout.NewInProcess()

		// Replica B: no DB, no entities — just a bus bridged to the fanout.
		appB := NewApp(WithFanout(f), WithoutDefaultMiddleware())
		got := make(chan event.Event, 4)
		remote := make(chan bool, 4)
		appB.Events().On(event.EntityCreated, func(ctx context.Context, e event.Event) error {
			got <- e
			remote <- event.IsRemote(ctx)
			return nil
		})

		// Replica A: handles the write.
		appA := NewApp(WithDB(db), WithFanout(f), WithoutDefaultMiddleware())
		appA.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, appA.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(handler.SetUser(r.Context(), struct{ ID string }{ID: "u1"}))
		appA.Router().ServeHTTP(rec, r)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", rec.Code, rec.Body)
		}

		select {
		case e := <-got:
			data, ok := e.Data.(map[string]any)
			if !ok {
				t.Fatalf("remote event data type %T, want map[string]any", e.Data)
			}
			if data["entity"] != "posts" {
				t.Errorf("remote event entity = %v, want posts", data["entity"])
			}
		case <-time.After(3 * time.Second):
			t.Fatal("event emitted on replica A never reached replica B's bus")
		}
		if !<-remote {
			t.Error("event.IsRemote should be true in replica B's handler")
		}
	})
}

// fanoutHost is a Mountable that supports SetFanout, standing in for a
// mounted UI host.
type fanoutHost struct {
	wired   chan fanout.Fanout
	stopped chan struct{}
}

func (h *fanoutHost) Mount(_ *router.Router) {}
func (h *fanoutHost) SetFanout(f fanout.Fanout) (func(), error) {
	h.wired <- f
	return func() { close(h.stopped) }, nil
}

// TestWithFanout_WiresMountedHost proves Mount duck-types a SetFanout-capable
// Mountable into the app's fanout and that Shutdown detaches it.
func TestWithFanout_WiresMountedHost(t *testing.T) {
	f := fanout.NewInProcess()
	h := &fanoutHost{wired: make(chan fanout.Fanout, 1), stopped: make(chan struct{})}
	app := NewApp(WithFanout(f), WithoutDefaultMiddleware())
	app.Mount(h)

	select {
	case gotF := <-h.wired:
		if gotF != fanout.Fanout(f) {
			t.Error("host wired to a different fanout than WithFanout's")
		}
	default:
		t.Fatal("Mount did not wire the SetFanout-capable host")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-h.stopped:
	default:
		t.Error("Shutdown did not detach the mounted host's fanout wiring")
	}
}

func TestWithFanout_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithFanout(nil) should panic")
		}
	}()
	WithFanout(nil)
}

// TestWithFanout_GrantStorePropagates proves WithGrantStore wires the RBAC
// GrantStore into the app's shared fanout: a grant on replica A's store
// propagates to replica B's live policy without a restart.
func TestWithFanout_GrantStorePropagates(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // one shared in-memory DB for both stores
	ctx := context.Background()

	policyA := access.NewRolePolicy()
	storeA := access.NewGrantStore(db, policyA)
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}
	if err := storeA.LoadInto(ctx, policyA); err != nil {
		t.Fatalf("LoadInto A: %v", err)
	}
	policyB := access.NewRolePolicy()
	storeB := access.NewGrantStore(db, policyB)
	if err := storeB.LoadInto(ctx, policyB); err != nil {
		t.Fatalf("LoadInto B: %v", err)
	}

	f := fanout.NewInProcess()
	// WithGrantStore + WithFanout wires each store's SetFanout into the bus.
	appA := NewApp(WithFanout(f), WithGrantStore(storeA), WithoutDefaultMiddleware())
	appB := NewApp(WithFanout(f), WithGrantStore(storeB), WithoutDefaultMiddleware())
	// Shutdown detaches the wiring (exercises the OnStop stop() path).
	defer func() { _ = appA.Shutdown(ctx); _ = appB.Shutdown(ctx) }()

	if err := storeA.Grant(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("Grant on A: %v", err)
	}

	cB := access.WithRoles(access.WithPolicy(ctx, policyB), []string{"editor"})
	deadline := time.Now().Add(2 * time.Second)
	for !access.Can(cB, "posts:read") {
		if time.Now().After(deadline) {
			t.Fatal("grant on replica A did not propagate to replica B via WithGrantStore wiring")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// accessSubErrFanout succeeds for every topic EXCEPT the access lane, letting
// a test drive the access-fanout-subscribe error path without tripping the
// module lane first.
type accessSubErrFanout struct{}

func (accessSubErrFanout) Publish(context.Context, string, []byte) error { return nil }
func (accessSubErrFanout) Subscribe(topic string, _ func([]byte)) (func(), error) {
	if topic == "gofastr.access" {
		return nil, errors.New("subscribe boom")
	}
	return func() {}, nil
}

// TestWithGrantStore_FanoutSubscribeErrorPanics covers the wiring's failure
// path: if the GrantStore's SetFanout fails to subscribe, app construction
// panics with an actionable message rather than silently running unwired.
func TestWithGrantStore_FanoutSubscribeErrorPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when the access fanout Subscribe errors")
		}
		if !strings.Contains(fmt.Sprint(r), "access fanout subscribe") {
			t.Fatalf("panic = %v, want it to mention access fanout subscribe", r)
		}
	}()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Construction wires access.SetFanout, which errors → panic.
	NewApp(WithFanout(accessSubErrFanout{}), WithGrantStore(store), WithoutDefaultMiddleware())
}
