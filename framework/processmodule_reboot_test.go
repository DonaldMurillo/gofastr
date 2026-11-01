package framework

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A process module's desired-state row outlives the process. The second
// boot of an app against the same database re-registers the module, and
// the row it finds decides what happens: same artifact and grants keep the
// operator's enabled flag and generation; a new artifact keeps it enabled;
// changed grants disable it until an operator re-enables it.

// rebootBackend names one persistent database per case (fresh) and opens a
// new connection to it per boot (open), so each boot is a separate
// process's view of the same rows.
type rebootBackend struct {
	name  string
	fresh func(t *testing.T) string
	open  func(t *testing.T, loc string) *sql.DB
}

func rebootBackends() []rebootBackend {
	return []rebootBackend{
		{
			name:  "sqlite",
			fresh: func(t *testing.T) string { return "file:" + filepath.Join(t.TempDir(), "app.db") },
			open: func(t *testing.T, loc string) *sql.DB {
				db, err := sql.Open("sqlite3", loc)
				if err != nil {
					t.Skipf("sqlite3 driver not available: %v", err)
				}
				db.SetMaxOpenConns(1)
				return db
			},
		},
		{
			name:  "postgres",
			fresh: pgtest.FreshDatabaseDSN,
			open: func(t *testing.T, loc string) *sql.DB {
				db, err := sql.Open("postgres", loc)
				if err != nil {
					t.Fatalf("open postgres: %v", err)
				}
				return db
			},
		},
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bootProcessApp is one boot: a new App on db that registers d. A panic
// from RegisterProcessModule is returned as an error so the test names it.
func bootProcessApp(t *testing.T, db *sql.DB, d ProcessModuleDescriptor, approved ApprovedGrants, logs *syncBuffer) (app *App, err error) {
	t.Helper()
	app = NewApp(WithDB(db), WithLogger(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("RegisterProcessModule panicked: %v", r)
		}
	}()
	app.RegisterProcessModule(d, approved)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.ProcessModules().Close(ctx)
	})
	return app, nil
}

// TestProcessModuleRebootKeepsState: two boots against one database
// (file-backed SQLite and Postgres). The second boot must not panic, and
// the row it leaves behind follows the descriptor diff.
func TestProcessModuleRebootKeepsState(t *testing.T) {
	read := []access.Permission{"articles:read"}
	readWrite := []access.Permission{"articles:read", "articles:create"}
	oldSHA := strings.Repeat("ab", 32)

	type tc struct {
		name               string
		bootOneGrants      []access.Permission
		bootOneSHA         string // "" = the real demo binary's digest
		bootTwoGrants      []access.Permission
		wantEnabled        bool
		wantGen            uint64
		wantGrants         []access.Permission
		wantWarn           bool
		wantReadyAfterBoot bool
		// revokeBetween is revoked at runtime after boot one.
		revokeBetween []access.Permission
	}
	cases := []tc{
		{name: "same", bootOneGrants: read, bootTwoGrants: read, wantEnabled: true, wantGen: 1, wantGrants: read, wantReadyAfterBoot: true},
		{name: "new_artifact", bootOneGrants: read, bootOneSHA: oldSHA, bootTwoGrants: read, wantEnabled: true, wantGen: 2, wantGrants: read},
		{name: "grants_widened", bootOneGrants: read, bootTwoGrants: readWrite, wantEnabled: false, wantGen: 2, wantGrants: readWrite, wantWarn: true},
		// A narrower descriptor takes grants away and adds none: nothing new
		// runs unseen, so the module stays enabled (issue #614).
		{name: "grants_narrowed", bootOneGrants: readWrite, bootTwoGrants: read, wantEnabled: true, wantGen: 2, wantGrants: read},
		// A runtime revoke outlives the next boot: the descriptor still
		// requests the revoked grant, and the stored revoke wins.
		{name: "revoke_survives", bootOneGrants: readWrite, revokeBetween: []access.Permission{"articles:create"}, bootTwoGrants: readWrite, wantEnabled: true, wantGen: 2, wantGrants: read},
		// A grant the descriptor adds after a revoke still disables, and the
		// revoked grant stays out.
		{name: "revoke_then_widen", bootOneGrants: readWrite, revokeBetween: []access.Permission{"articles:create"}, bootTwoGrants: []access.Permission{"articles:read", "articles:create", "articles:update"}, wantEnabled: false, wantGen: 3, wantGrants: []access.Permission{"articles:read", "articles:update"}, wantWarn: true},
		{name: "grants_and_artifact", bootOneGrants: read, bootOneSHA: oldSHA, bootTwoGrants: readWrite, wantEnabled: false, wantGen: 2, wantGrants: readWrite, wantWarn: true},
		{name: "grants_reordered", bootOneGrants: readWrite, bootTwoGrants: []access.Permission{"articles:create", "articles:read", "articles:read"}, wantEnabled: true, wantGen: 1, wantGrants: readWrite},
	}
	for _, be := range rebootBackends() {
		t.Run(be.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					ctx := context.Background()
					// Boot one: register, enable, shut down.
					d1 := demoDescriptor(t, c.bootOneGrants, "", TrustTrusted)
					if c.bootOneSHA != "" {
						d1.ArtifactSHA256 = c.bootOneSHA
					}
					loc := be.fresh(t)
					db1 := be.open(t, loc)
					var logs1 syncBuffer
					app1, err := bootProcessApp(t, db1, d1, ApprovedGrants(c.bootOneGrants), &logs1)
					if err != nil {
						t.Fatalf("first boot: %v", err)
					}
					if err := app1.ProcessModules().Enable(ctx, d1.Name); err != nil {
						t.Fatalf("first boot enable: %v", err)
					}
					if c.revokeBetween != nil {
						if _, err := app1.ProcessModules().RevokeGrants(ctx, d1.Name, c.revokeBetween); err != nil {
							t.Fatalf("first boot revoke: %v", err)
						}
					}
					closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_ = app1.ProcessModules().Close(closeCtx)
					cancel()
					_ = db1.Close()

					// Boot two: same database, new process.
					d2 := demoDescriptor(t, c.bootTwoGrants, "", TrustTrusted)
					db2 := be.open(t, loc)
					t.Cleanup(func() { _ = db2.Close() })
					var logs2 syncBuffer
					app2, err := bootProcessApp(t, db2, d2, ApprovedGrants(c.bootTwoGrants), &logs2)
					if err != nil {
						t.Fatalf("second boot: %v", err)
					}
					store, err := NewSQLProcessModuleStore(db2)
					if err != nil {
						t.Fatalf("store: %v", err)
					}
					row, err := store.GetDesired(ctx, d2.Name)
					if err != nil {
						t.Fatalf("get desired: %v", err)
					}
					if row.Enabled != c.wantEnabled {
						t.Errorf("row enabled = %v, want %v", row.Enabled, c.wantEnabled)
					}
					if row.DesiredGeneration != c.wantGen {
						t.Errorf("row generation = %d, want %d", row.DesiredGeneration, c.wantGen)
					}
					if row.ArtifactSHA256 != d2.ArtifactSHA256 {
						t.Errorf("row artifact sha = %s, want the second boot's %s", row.ArtifactSHA256, d2.ArtifactSHA256)
					}
					if !slices.Equal(sortedGrants(row.EffectiveGrants), sortedGrants(c.wantGrants)) {
						t.Errorf("row grants = %v, want %v", row.EffectiveGrants, c.wantGrants)
					}
					// The route gate reads the stored flag before the
					// first reconcile, not a false default.
					if got := app2.Modules().Enabled(d2.Name); got != c.wantEnabled {
						t.Errorf("Modules().Enabled after boot = %v, want %v", got, c.wantEnabled)
					}
					warned := strings.Contains(logs2.String(), "level=WARN") &&
						strings.Contains(logs2.String(), "module="+d2.Name) &&
						strings.Contains(logs2.String(), "grants changed")
					if warned != c.wantWarn {
						t.Errorf("grants-changed Warn logged = %v, want %v; logs:\n%s", warned, c.wantWarn, logs2.String())
					}
					if c.wantReadyAfterBoot {
						if err := app2.InitPlugins(); err != nil {
							t.Fatalf("InitPlugins: %v", err)
						}
						app2.ProcessModules().StartLoops()
						if code := waitStatus(app2, "/demo/hello", http.StatusOK, 10*time.Second); code != http.StatusOK {
							t.Errorf("second boot: enabled module route = %d, want 200 once Ready", code)
						}
					}
				})
			}
		})
	}
}

// flakyInstallStore fails the first InstallOrUpdate, then delegates.
type flakyInstallStore struct {
	*SQLProcessModuleStore
	failed bool
}

func (s *flakyInstallStore) InstallOrUpdate(ctx context.Context, d DesiredState) (InstallResult, error) {
	if !s.failed {
		s.failed = true
		return InstallResult{}, errors.New("store unreachable")
	}
	return s.SQLProcessModuleStore.InstallOrUpdate(ctx, d)
}

// TestRegisterRetriesAfterStoreError: a Register the store refused leaves
// no slot behind, so the retry registers instead of reporting a duplicate.
func TestRegisterRetriesAfterStoreError(t *testing.T) {
	store := &flakyInstallStore{SQLProcessModuleStore: newTestStore(t)}
	sup := newGateSupervisor(t, store, NopBroker{}, &demoRunner{}, "replica-1")
	ctx := context.Background()
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", TrustTrusted)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err == nil {
		t.Fatal("register with the store down: want an error")
	}
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register retry after the store recovered: %v", err)
	}
}

// TestDuplicateRegisterLeavesRow: registering a name twice in one process
// is still refused, and the refused call writes nothing to the row.
func TestDuplicateRegisterLeavesRow(t *testing.T) {
	store := newTestStore(t)
	sup := newGateSupervisor(t, store, NopBroker{}, &demoRunner{}, "replica-1")
	ctx := context.Background()
	d := demoDescriptor(t, []access.Permission{"articles:read"}, "", TrustTrusted)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := sup.Enable(ctx, d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	wider := demoDescriptor(t, []access.Permission{"articles:read", "articles:create"}, "", TrustTrusted)
	if _, err := sup.Register(ctx, wider, ApprovedGrants{"articles:read", "articles:create"}); !errors.Is(err, ErrModuleInstalled) {
		t.Fatalf("duplicate register in one process: want ErrModuleInstalled, got %v", err)
	}
	row, err := store.GetDesired(ctx, d.Name)
	if err != nil {
		t.Fatalf("get desired: %v", err)
	}
	if !row.Enabled || row.DesiredGeneration != 1 || len(row.EffectiveGrants) != 1 {
		t.Errorf("refused duplicate register rewrote the row: %+v", row)
	}
}

func sortedGrants(g []access.Permission) []access.Permission {
	out := slices.Clone(g)
	slices.Sort(out)
	return out
}
