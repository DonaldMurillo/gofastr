package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestWithFanout_CrossAppDelivery drives the full seam: a CRUD write on app
// A emits on A's bus, the bridge mirrors it through the shared fanout, and a
// plain bus subscriber on app B (a second "replica") receives it, marked
// remote.
func TestWithFanout_CrossAppDelivery(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		f := fanout.NewInProcess()

		// Replica B: no DB, no entities, just a bus bridged to the fanout.
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

type blockingPresenceFanout struct {
	fanout.Fanout
	blockGoodbye bool
	entered      chan context.Context
	release      chan struct{}
	detached     chan struct{}
	goodbyes     atomic.Int32
}

func (f *blockingPresenceFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	if topic == "gofastr.presence" {
		_, body, err := fanout.Unwrap(payload)
		var msg struct {
			Members []json.RawMessage `json:"m"`
		}
		if err == nil && json.Unmarshal(body, &msg) == nil {
			goodbye := len(msg.Members) == 0
			if goodbye {
				f.goodbyes.Add(1)
			}
			if goodbye == f.blockGoodbye {
				f.entered <- ctx
				<-f.release
			}
		}
	}
	return f.Fanout.Publish(ctx, topic, payload)
}

func (f *blockingPresenceFanout) Subscribe(topic string, fn func([]byte)) (func(), error) {
	cancel, err := f.Fanout.Subscribe(topic, fn)
	if err != nil || topic != "gofastr.presence" {
		return cancel, err
	}
	return func() {
		cancel()
		close(f.detached)
	}, nil
}

// waitIslandsDetached waits until the manager accepts a new fanout, which
// it refuses while the previous detach is still running.
func waitIslandsDetached(t *testing.T, m *island.Manager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stop, err := m.SetFanout(fanout.NewInProcess())
		if err == nil {
			stop()
			return
		}
		if time.Now().After(deadline) {
			t.Error("island fanout did not detach after Publish was released")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestShutdownBoundsIslandFanoutStop(t *testing.T) {
	for _, tc := range []struct {
		name         string
		blockGoodbye bool
		expired      bool
	}{
		{name: "queued roster"},
		{name: "goodbye", blockGoodbye: true},
		{name: "already expired", expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &blockingPresenceFanout{
				Fanout: fanout.NewInProcess(), blockGoodbye: tc.blockGoodbye,
				entered: make(chan context.Context, 1), release: make(chan struct{}), detached: make(chan struct{}),
			}
			a := NewApp(WithFanout(f), WithSecret(strings.Repeat("s", 32)), WithoutDefaultMiddleware())
			host := uihost.New(uiapp.NewApp("presence"))
			a.Mount(host)
			joined := host.Islands.PresenceJoin("session", island.PresenceIdentity{UserID: "user"}, []string{"doc:1"})
			defer joined.Leave()
			defer func() {
				close(f.release)
				waitIslandsDetached(t, host.Islands)
				want := int32(0)
				if tc.blockGoodbye {
					want = 1
				}
				if got := f.goodbyes.Load(); got != want {
					t.Errorf("goodbyes = %d, want %d", got, want)
				}
			}()
			waitForPublish := func() context.Context {
				t.Helper()
				select {
				case ctx := <-f.entered:
					return ctx
				case <-time.After(2 * time.Second):
					t.Fatal("presence Publish did not start")
					return nil
				}
			}
			timeout := 100 * time.Millisecond
			if tc.expired {
				timeout = -time.Second
			}
			if !tc.blockGoodbye {
				waitForPublish()
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- a.Shutdown(ctx) }()
			if tc.blockGoodbye {
				publishCtx := waitForPublish()
				got, _ := publishCtx.Deadline()
				want, _ := ctx.Deadline()
				if !got.Equal(want) {
					t.Errorf("goodbye deadline = %v, want shutdown deadline %v", got, want)
				}
			}
			select {
			case err := <-done:
				const msg = "island fanout stop abandoned after the drain deadline"
				if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), msg) {
					t.Fatalf("Shutdown = %v, want %q wrapping context.DeadlineExceeded", err, msg)
				}
			case <-time.After(time.Second):
				t.Fatal("Shutdown waited past its deadline for blocked presence Publish")
			}
			select {
			case <-f.detached:
			case <-time.After(2 * time.Second):
				t.Fatal("the presence subscription is still attached after Shutdown abandoned the island fanout stop")
			}
		})
	}
}

func TestShutdownDetachesIslandFanoutInOrder(t *testing.T) {
	f := &blockingPresenceFanout{
		Fanout: fanout.NewInProcess(), entered: make(chan context.Context, 1),
		release: make(chan struct{}), detached: make(chan struct{}),
	}
	close(f.release)
	a := NewApp(WithFanout(f), WithSecret(strings.Repeat("s", 32)), WithoutDefaultMiddleware())
	a.OnStop(func() error {
		select {
		case <-f.detached:
			if n := f.goodbyes.Load(); n != 1 {
				return fmt.Errorf("goodbyes before OnStop = %d, want 1", n)
			}
			return nil
		default:
			return errors.New("island fanout is still attached during an earlier OnStop hook")
		}
	})
	host := uihost.New(uiapp.NewApp("presence"))
	a.Mount(host)
	joined := host.Islands.PresenceJoin("session", island.PresenceIdentity{UserID: "user"}, []string{"doc:1"})
	defer joined.Leave()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatal(err)
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

type blockingEventFanout struct {
	publishing chan struct{}
	release    chan struct{}
	detached   chan struct{}
}

func (f *blockingEventFanout) Publish(context.Context, string, []byte) error {
	close(f.publishing)
	<-f.release
	return nil
}

func (f *blockingEventFanout) Subscribe(string, func([]byte)) (func(), error) {
	return func() { close(f.detached) }, nil
}

func TestEventBridgeShutdownDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{
		{"in-flight", time.Second},
		{"already-expired", -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &blockingEventFanout{
					publishing: make(chan struct{}),
					release:    make(chan struct{}),
					detached:   make(chan struct{}),
				}
				defer close(f.release)
				app := NewApp(WithFanout(f), WithoutDefaultMiddleware())
				finalHookRan := make(chan struct{})
				app.OnStopFirst(func() error {
					close(finalHookRan)
					if tc.timeout <= 0 {
						// An expired drain starts the stop without waiting for it.
						return nil
					}
					select {
					case <-f.detached:
					default:
						t.Error("event bridge was not detached before the final stop hook")
					}
					return nil
				})
				if err := app.Events().Emit(context.Background(), event.Event{Type: "ping"}); err != nil {
					t.Fatal(err)
				}
				<-f.publishing

				ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- app.Shutdown(ctx) }()

				if tc.timeout > 0 {
					synctest.Wait()
					select {
					case err := <-done:
						t.Fatalf("Shutdown returned before its deadline: %v", err)
					default:
					}
				}
				<-ctx.Done()
				synctest.Wait()
				select {
				case err := <-done:
					if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "event bridge stop abandoned after the drain deadline") {
						t.Errorf("Shutdown = %v, want context.DeadlineExceeded from the event bridge stop", err)
					}
					select {
					case <-finalHookRan:
					default:
						t.Error("Shutdown skipped the final stop hook")
					}
					select {
					case <-f.detached:
					default:
						t.Error("Shutdown did not start the event bridge stop")
					}
				default:
					t.Error("Shutdown still waiting for fanout Publish after its deadline")
				}
			})
		})
	}
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
