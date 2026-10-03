package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// nil2Ctx is a background context stand-in for extractor calls.
func nil2Ctx() context.Context { return context.Background() }

// restoreExtractor snapshots the process-wide owner extractor and
func restoreExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	t.Cleanup(func() {
		owner.SetExtractor(prev)
	})
}

// Group 2: the local identity.

func TestIdentityFileMinted0600AndStable(t *testing.T) {
	dir := t.TempDir()
	u1, err := loadOrMintLocalUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if u1.id == "" {
		t.Fatal("minted identity is empty")
	}
	info, err := os.Stat(filepath.Join(dir, identityFileName))
	if err != nil {
		t.Fatalf("identity file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("identity file mode = %o, want 0600", perm)
	}
	// A second load (fresh battery) sees the same id.
	dirMode, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = dirMode
	u2, err := loadOrMintLocalUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if u1.id != u2.id {
		t.Fatalf("identity not stable: %q vs %q", u1.id, u2.id)
	}
	// The structural battery/auth User contract.
	if u1.GetEmail() != "local@"+u1.id {
		t.Errorf("GetEmail = %q", u1.GetEmail())
	}
	// KEPT, deliberately, by the 2026-09-04 security pass. The local
	// identity being an admin is right for the single-user, auth-free
	// desktop app it exists for. What was wrong was the SCOPE of the
	// fallback: it also spoke for requests battery/auth had already
	// marked anonymous, making a signed-out window an administrator.
	// That is fixed in localUserMiddleware (Battery.authOwnsIdentity)
	// and bounded by TestAnonDecisionIsNotOverridden in
	// localuser_security_test.go, which is the test that now says WHEN
	// this role may be handed out. The role itself did not change.
	if r := u1.GetRoles(); len(r) != 1 || r[0] != "admin" {
		t.Errorf("GetRoles = %v, want [admin]", r)
	}
}

func TestDataDirCreated0700(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	t.Setenv("HOME", filepath.Join(base, "home"))
	dir, err := DataDir("notes.example.app")
	if err != nil {
		t.Fatal(err)
	}
	xdgBase := filepath.Join(base, "xdg")
	for d := dir; len(d) >= len(xdgBase); d = filepath.Dir(d) {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("%s mode = %o, want no group/other bits", d, perm)
		}
	}
}

func TestLocalUserMiddlewareSetsContextUser(t *testing.T) {
	b, _ := newTestBattery(t)
	b.user = &localUser{id: "id123"}
	b.armGate() // desktop mode: Run arms the gate before the listener opens
	var got any
	var ok bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = handler.GetUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	b.localUserMiddleware()(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !ok {
		t.Fatal("handler.GetUser not set")
	}
	u, isLocal := got.(*localUser)
	if !isLocal || u.id != "id123" {
		t.Fatalf("context user = %#v", got)
	}
}

func TestOwnerExtractorInstalledWhenAbsent(t *testing.T) {
	restoreExtractor(t)
	owner.SetExtractor(nil)
	b, _ := newTestBattery(t)
	b.user = &localUser{id: "own-id"}
	b.installOwnerExtractor()
	if owner.GetExtractor() == nil {
		t.Fatal("extractor not installed")
	}
	id, ok := owner.Get(handler.SetUser(nil2Ctx(), b.user))
	if !ok || id != "own-id" {
		t.Fatalf("owner.Get = (%v, %v), want (own-id, true)", id, ok)
	}
}

func TestOwnerExtractorNotReplacedWhenPresent(t *testing.T) {
	restoreExtractor(t)
	sentinel := func(ctx context.Context) (any, bool) { return "sentinel", true }
	owner.SetExtractor(sentinel)
	b, _ := newTestBattery(t)
	b.user = &localUser{id: "should-not-win"}
	b.installOwnerExtractor()
	id, ok := owner.Get(nil2Ctx())
	if !ok || id != "sentinel" {
		t.Fatalf("existing extractor replaced: owner.Get = (%v, %v)", id, ok)
	}
}

// The local identity is a desktop-mode fact. With the gate unarmed (an
// app serving itself over plain HTTP behind --serve, where any browser
// can reach the port) the middleware must inject nothing: otherwise
// every anonymous visitor would be the local admin.
func TestLocalUserNotInjectedWhenGateUnarmed(t *testing.T) {
	b, _ := newTestBattery(t)
	b.user = &localUser{id: "id123"}
	var ok bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = handler.GetUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	b.localUserMiddleware()(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ok {
		t.Fatal("local identity injected while the boot gate is unarmed (serve mode)")
	}
}
