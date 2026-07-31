package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// captureSlog routes slog.Default through a buffer for the duration of fn.
func captureSlog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// Production mode on the default in-memory session store is the silent
// multi-replica failure: replica B never resolves replica A's cookie,
// and nothing survives a restart. A warn-only Init lets a broken
// deployment boot undiscovered — so, like the in-memory 2FA store,
// production must REFUSE to boot unless the host explicitly opts in via
// AllowInMemoryStores.
func TestProdMemorySessionStoreFailsClosed(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{JWTSecret: "k"}) // prod mode, default memory session store
	mgr.Use(auth.NewCorePlugin())
	err := mgr.Init(nil)
	if err == nil {
		t.Fatal("production Init with the default in-memory session store must fail closed")
	}
	for _, want := range []string{"in-memory session store", "AllowInMemoryStores"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must name %q; got: %v", want, err)
		}
	}
}

func TestNoMemoryStoreWarnWhenOptedIn(t *testing.T) {
	out := captureSlog(t, func() {
		mgr := auth.New(auth.AuthConfig{JWTSecret: "k", AllowInMemoryStores: true})
		mgr.Use(auth.NewCorePlugin())
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})
	if strings.Contains(out, "in-memory session store") {
		t.Fatalf("explicit AllowInMemoryStores should silence the warning; got log: %q", out)
	}
}

func TestNoMemoryStoreWarnInDevMode(t *testing.T) {
	out := captureSlog(t, func() {
		mgr := auth.New(auth.AuthConfig{DevMode: true})
		mgr.Use(auth.NewCorePlugin())
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})
	if strings.Contains(out, "in-memory session store") {
		t.Fatalf("DevMode should not warn about in-memory sessions; got log: %q", out)
	}
}

// The default in-memory 2FA store is worse than a scaling gap: a restart
// wipes enrollment, silently switching every account back to
// password-only. A security control that quietly stops applying is not
// warning-grade — production must refuse to boot.
func TestProdRefusesMemoryTwoFAStore(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{JWTSecret: "k", SessionStore: stubDurableSessions{}}) // durable sessions → Init reaches the 2FA gate, not the session gate
	mgr.Use(auth.NewCorePlugin())
	mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{}))
	err := mgr.Init(nil)
	if err == nil {
		t.Fatal("production Init with in-memory 2FA state must fail closed")
	}
	if !strings.Contains(err.Error(), "in-memory 2FA store") {
		t.Fatalf("refusal must name the cause; got: %v", err)
	}
}

// AllowInMemoryStores acknowledges single-node deployments: boot
// proceeds, but the downgrade risk still leaves a trace in the log.
func TestProdMemoryTwoFAStoreOptIn(t *testing.T) {
	out := captureSlog(t, func() {
		mgr := auth.New(auth.AuthConfig{JWTSecret: "k", AllowInMemoryStores: true})
		mgr.Use(auth.NewCorePlugin())
		mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{}))
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("Init with AllowInMemoryStores: %v", err)
		}
	})
	if !strings.Contains(out, "in-memory 2FA store") {
		t.Fatalf("acknowledged in-memory 2FA should still log a warning; got: %q", out)
	}
}

// DevMode keeps the zero-config on-ramp: memory 2FA store, no error, no
// warning.
func TestDevModeAllowsMemoryTwoFAStore(t *testing.T) {
	out := captureSlog(t, func() {
		mgr := auth.New(auth.AuthConfig{DevMode: true})
		mgr.Use(auth.NewCorePlugin())
		mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{}))
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})
	if strings.Contains(out, "in-memory 2FA store") {
		t.Fatalf("DevMode should not warn about in-memory 2FA; got: %q", out)
	}
}

// storeSwappingPlugin installs the in-memory session store from its Init,
// which runs AFTER the manager's production check.
type storeSwappingPlugin struct{}

func (storeSwappingPlugin) Name() string { return "store-swapper" }

func (storeSwappingPlugin) Init(mgr *auth.AuthManager) error {
	mgr.SetSessionStore(auth.NewMemorySessionStore())
	return nil
}

func (storeSwappingPlugin) RegisterRoutes(*router.Router) {}

// durableStoreStub stands in for a real backing store: anything that is not
// *MemorySessionStore passes the production check.
type durableStoreStub struct{}

func (durableStoreStub) Create(context.Context, string, time.Duration) (*auth.Session, error) {
	return &auth.Session{}, nil
}
func (durableStoreStub) Get(context.Context, string) (*auth.Session, error) { return nil, nil }
func (durableStoreStub) Delete(context.Context, string) error               { return nil }
func (durableStoreStub) Cleanup(context.Context) (int, error)               { return 0, nil }

// The production refusal has to describe the store the app will actually
// RUN with. Plugin Init runs after the check, so a plugin that swaps in the
// in-memory store used to sail past it: Init returned nil with production
// mode active, AllowInMemoryStores false, and every session in RAM.
func TestProdRefusesMemorySessionStoreInstalledByPlugin(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{JWTSecret: "k"})
	mgr.SetSessionStore(durableStoreStub{})
	mgr.Use(storeSwappingPlugin{})
	mgr.Use(auth.NewCorePlugin())

	err := mgr.Init(nil)
	if err == nil {
		t.Fatal("production Init must refuse an in-memory session store installed by a plugin")
	}
	if !strings.Contains(err.Error(), "in-memory session store") {
		t.Errorf("refusal must name the in-memory session store; got: %v", err)
	}
}
