package auth_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
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
// multi-replica failure: replica B never resolves replica A's cookie.
// Init must say so loudly unless the host explicitly opted into
// single-node in-memory state.
func TestProdWarnsOnMemorySessionStore(t *testing.T) {
	out := captureSlog(t, func() {
		mgr := auth.New(auth.AuthConfig{JWTSecret: "k"})
		mgr.Use(auth.NewCorePlugin())
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})
	if !strings.Contains(out, "in-memory session store") {
		t.Fatalf("production Init with in-memory sessions must warn; got log: %q", out)
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
	mgr := auth.New(auth.AuthConfig{JWTSecret: "k"})
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
