package framework

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// hex256 mints a 64-char hex string for tests (deterministic prefix + pad).
func hex256(seed string) string {
	if len(seed) >= 64 {
		return seed[:64]
	}
	return seed + strings.Repeat("0", 64-len(seed))
}

// validDescriptor builds a minimal descriptor that passes
// [ValidateProcessModuleDescriptor] for the "demo" module. Tests clone +
// tweak it.
func validDescriptor() ProcessModuleDescriptor {
	return ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactPath:   "/dev/null",
		ArtifactSHA256: hex256("a1"),
		SurfaceSHA256:  hex256("b2"),
		Routes: []RouteDeclaration{
			{ID: "list", Method: "GET", Path: "/items"},
			{ID: "get", Method: "GET", Path: "/items/:id"},
		},
		RequestedGrants: []access.Permission{
			"articles:read",
			"articles:write",
		},
		TrustTier: TrustTrusted,
	}
}

// ---- descriptor validation ----

func TestValidateDescriptor_ok(t *testing.T) {
	d := validDescriptor()
	eff, err := ValidateProcessModuleDescriptor(d, ApprovedGrants{
		"articles:read", "articles:write", "extra:thing",
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(eff) != 2 {
		t.Errorf("effective grants = %v, want 2", eff)
	}
}

func TestValidateDescriptor_rejectsNonGrantable(t *testing.T) {
	cases := []access.Permission{
		"CrossOwnerRead",
		"*:*",
		"*",
		"*:read",
	}
	for _, g := range cases {
		d := validDescriptor()
		d.RequestedGrants = []access.Permission{"articles:read", g}
		_, err := ValidateProcessModuleDescriptor(d, ApprovedGrants{"articles:read", g})
		if err == nil {
			t.Errorf("grant %q should be rejected by carve-out", g)
			continue
		}
		var dv *DescriptorValidationError
		if !errors.As(err, &dv) {
			t.Errorf("grant %q: want DescriptorValidationError, got %T(%v)", g, err, err)
			continue
		}
		if dv.Rule != "non_grantable" {
			t.Errorf("grant %q: rule = %q, want non_grantable", g, dv.Rule)
		}
	}
}

func TestValidateDescriptor_allowsResourceWildcard(t *testing.T) {
	// Resource-scoped wildcards are GRANTABLE: they cannot subsume a
	// cross-owner verb (design §5). "articles:*" grants articles:read etc
	// but not "articles:read:all" (different scope).
	d := validDescriptor()
	d.RequestedGrants = []access.Permission{"articles:*"}
	eff, err := ValidateProcessModuleDescriptor(d, ApprovedGrants{"articles:*"})
	if err != nil {
		t.Fatalf("resource wildcard should be allowed: %v", err)
	}
	if len(eff) != 1 || eff[0] != "articles:*" {
		t.Errorf("effective = %v, want [articles:*]", eff)
	}
}

func TestValidateDescriptor_rejectsBadScope(t *testing.T) {
	d := validDescriptor()
	d.RequestedGrants = []access.Permission{"no-colon"}
	_, err := ValidateProcessModuleDescriptor(d, ApprovedGrants{"no-colon"})
	if err == nil {
		t.Fatal("malformed scope should be rejected")
	}
}

func TestValidateDescriptor_rejectsGrantCap(t *testing.T) {
	d := validDescriptor()
	d.RequestedGrants = make([]access.Permission, maxModuleGrants+1)
	for i := range d.RequestedGrants {
		d.RequestedGrants[i] = access.Permission(fmt.Sprintf("r%d:read", i))
	}
	_, err := ValidateProcessModuleDescriptor(d, nil)
	if err == nil {
		t.Fatal("grant cap exceeded should be rejected")
	}
}

func TestValidateDescriptor_rejectsDigestShape(t *testing.T) {
	cases := []struct {
		field string
		mod   func(d *ProcessModuleDescriptor)
	}{
		{"artifact_sha256", func(d *ProcessModuleDescriptor) { d.ArtifactSHA256 = "deadbeef" }},
		{"surface_sha256", func(d *ProcessModuleDescriptor) { d.SurfaceSHA256 = "" }},
		{"artifact_sha256", func(d *ProcessModuleDescriptor) { d.ArtifactSHA256 = "zz" + strings.Repeat("0", 62) }},
	}
	for _, c := range cases {
		d := validDescriptor()
		c.mod(&d)
		_, err := ValidateProcessModuleDescriptor(d, nil)
		if err == nil {
			t.Errorf("case %q: expected error", c.field)
			continue
		}
		var dv *DescriptorValidationError
		if !errors.As(err, &dv) || !strings.HasPrefix(dv.Field, c.field) {
			t.Errorf("case %q: want field %q, got %v", c.field, c.field, err)
		}
	}
}

func TestValidateDescriptor_rejectsRaisedLimits(t *testing.T) {
	d := validDescriptor()
	d.Limits.Deadline = maxModuleCallDeadline + time.Second
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("deadline above ceiling should be rejected")
	}
	d = validDescriptor()
	d.Limits.FrameBytes = moduleproto.DefaultMaxFrameBytes + 1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("frame bytes above default should be rejected")
	}
	d = validDescriptor()
	d.Limits.Inflight = moduleproto.DefaultMaxInflight + 1
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Error("inflight above default should be rejected")
	}
}

func TestValidateDescriptor_rejectsDuplicateRouteID(t *testing.T) {
	d := validDescriptor()
	d.Routes = []RouteDeclaration{
		{ID: "dup", Method: "GET", Path: "/a"},
		{ID: "dup", Method: "GET", Path: "/b"},
	}
	if _, err := ValidateProcessModuleDescriptor(d, nil); err == nil {
		t.Fatal("duplicate route id should be rejected")
	}
}

func TestComputeSurfaceSHA256_stable(t *testing.T) {
	d := validDescriptor()
	h1, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("surface SHA not stable: %s vs %s", h1, h2)
	}
	if len(h1) != hexSHA256Len {
		t.Errorf("surface SHA len = %d, want %d", len(h1), hexSHA256Len)
	}
	// A different surface must yield a different digest.
	d2 := d
	d2.Version = "2.0.0"
	h3, _ := ComputeSurfaceSHA256(d2)
	if h3 == h1 {
		t.Error("surface SHA should change when version changes")
	}
}

// ---- broker ----

func TestNopBroker_deniesReverseCalls(t *testing.T) {
	// Build a host+child pair over net.Pipe and install NopBroker on the
	// host. The child issues host.entity.query and must get an error.
	host, child, cleanup := newModuleProtoPipe(t)
	defer cleanup()
	(NopBroker{}).InstallHandlers(host, ModuleGrantView{
		Name: "demo", Grants: []access.Permission{"articles:read"}, Generation: 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := child.Call(ctx, moduleproto.MethodHostEntityQuery,
		moduleproto.EntityQueryParams{Entity: "articles"})
	if err == nil {
		t.Fatalf("NopBroker should deny; got result %s", raw)
	}
	we := moduleproto.AsError(err)
	if we == nil {
		t.Fatalf("want *moduleproto.Error, got %T(%v)", err, err)
	}
	if we.Code != moduleproto.CodeInternalError {
		t.Errorf("code = %d, want %d", we.Code, moduleproto.CodeInternalError)
	}
}

// ---- store ----

// newStoreDB opens a SQLite DB suitable for supervisor tests: shared-cache
// in-memory so every connection from the *sql.DB pool sees the same schema
// and rows (the default ":memory:" gives each connection its own private
// DB, which breaks the supervisor's pooled reads). The DSN is unique per
// call so concurrent tests don't collide.
var storeDBCounter atomic.Uint64

func newStoreDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:pmstore%d?mode=memory&cache=shared", storeDBCounter.Add(1))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Skipf("sqlite3 driver not available: %v", err)
	}
	// Single shared connection: required for cache=shared to be observable
	// across the pool, and matches the in-process SQLModuleStore's pattern.
	db.SetMaxOpenConns(1)
	return db
}

func TestProcessModuleStore_installGetBump(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	d := DesiredState{
		Module: "demo", DesiredGeneration: 1, Enabled: false,
		ArtifactSHA256:  hex256("aa"),
		EffectiveGrants: []access.Permission{"articles:read"},
	}
	if err := store.Install(ctx, d); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Re-install fails.
	if err := store.Install(ctx, d); !errors.Is(err, ErrModuleInstalled) {
		t.Fatalf("re-install: want ErrModuleInstalled, got %v", err)
	}
	got, err := store.GetDesired(ctx, "demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DesiredGeneration != 1 || got.Enabled {
		t.Errorf("get = %+v", got)
	}
	if len(got.EffectiveGrants) != 1 || got.EffectiveGrants[0] != "articles:read" {
		t.Errorf("grants = %v", got.EffectiveGrants)
	}
	// Unknown module → ErrNoDesiredRow.
	if _, err := store.GetDesired(ctx, "nope"); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("get unknown: want ErrNoDesiredRow, got %v", err)
	}
	// Bump generation.
	gen, err := store.BumpGeneration(ctx, "demo")
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if gen != 2 {
		t.Errorf("gen = %d, want 2", gen)
	}
	got, _ = store.GetDesired(ctx, "demo")
	if got.DesiredGeneration != 2 {
		t.Errorf("after bump: gen = %d", got.DesiredGeneration)
	}
}

func TestProcessModuleStore_setEnabledNoBump(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa")})
	if err := store.SetEnabled(ctx, "demo", true); err != nil {
		t.Fatalf("setEnabled: %v", err)
	}
	got, _ := store.GetDesired(ctx, "demo")
	if !got.Enabled {
		t.Error("enabled not persisted")
	}
	if got.DesiredGeneration != 1 {
		t.Errorf("SetEnabled must not bump gen: got %d", got.DesiredGeneration)
	}
}

func TestProcessModuleStore_setEffectiveGrantsBumps(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa")})
	gen, err := store.SetEffectiveGrants(ctx, "demo", []access.Permission{"x:read"})
	if err != nil {
		t.Fatalf("setEffectiveGrants: %v", err)
	}
	if gen != 2 {
		t.Errorf("gen = %d, want 2 (grant change bumps)", gen)
	}
	got, _ := store.GetDesired(ctx, "demo")
	if len(got.EffectiveGrants) != 1 || got.EffectiveGrants[0] != "x:read" {
		t.Errorf("grants = %v", got.EffectiveGrants)
	}
}

func TestProcessModuleStore_heartbeatsTTL(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa")})
	if err := store.RecordHeartbeat(ctx, "demo", "rep1", 1, "Ready"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.RecordHeartbeat(ctx, "demo", "rep2", 1, "Ready"); err != nil {
		t.Fatalf("record rep2: %v", err)
	}
	live, err := store.LiveReplicas(ctx, "demo", 10*time.Second)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("live replicas = %d, want 2", len(live))
	}
	// TTL=0 → no rows are live (updated_at is ~now, cutoff is now-0).
	live, _ = store.LiveReplicas(ctx, "demo", -1*time.Second)
	if len(live) != 0 {
		t.Errorf("TTL=-1s: live = %d, want 0", len(live))
	}
	// Delete one.
	_ = store.DeleteHeartbeat(ctx, "demo", "rep1")
	live, _ = store.LiveReplicas(ctx, "demo", 10*time.Second)
	if len(live) != 1 || live[0].ReplicaID != "rep2" {
		t.Errorf("after delete: live = %v", live)
	}
}

// newTestStore constructs a SQLite-backed store with EnsureSchema applied.
func newTestStore(t *testing.T) *SQLProcessModuleStore {
	t.Helper()
	db := newStoreDB(t)
	store, err := NewSQLProcessModuleStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return store
}

// newModuleProtoPipe is the framework-root test helper that wires a host +
// child Peer over an in-memory pipe. Mirrors core/moduleproto's newPeerPair.
func newModuleProtoPipe(t *testing.T) (host, child *moduleproto.Peer, _ func()) {
	t.Helper()
	connA, connB := pipeConn(t)
	codecA, err := moduleproto.NewCodec(connA, connA, 0)
	if err != nil {
		t.Fatalf("codecA: %v", err)
	}
	codecB, err := moduleproto.NewCodec(connB, connB, 0)
	if err != nil {
		t.Fatalf("codecB: %v", err)
	}
	host = moduleproto.NewPeer(codecA, moduleproto.RoleHost)
	child = moduleproto.NewPeer(codecB, moduleproto.RoleChild)
	host.Start()
	child.Start()
	cleanup := func() {
		_ = host.Close()
		_ = child.Close()
		_ = connA.Close()
		_ = connB.Close()
		<-host.Done()
		<-child.Done()
	}
	return host, child, cleanup
}

// ---- keep these imports used even when -tags short skips integration ----

var _ = json.Marshal
var _ = os.Getenv
var _ = fmt.Sprintf
var _ = hex.EncodeToString

// pipeConn returns two connected net.Conns for an in-memory codec pair.
func pipeConn(t *testing.T) (interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}, interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}) {
	t.Helper()
	a, b := net.Pipe()
	return a, b
}
