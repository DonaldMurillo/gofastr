package harness

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
)

type panicStore struct{ session.Store }

func (panicStore) AppendEvent(context.Context, control.EventEnvelope) error { panic("boom") }

func TestAppendEventPanicBecomesError(t *testing.T) {
	h := &Harness{Sessions: panicStore{}}
	if err := h.appendEventGuarded(context.Background(), control.EventEnvelope{}); err == nil {
		t.Fatal("a panicking AppendEvent returned no error")
	}
}

func TestSaltFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	path := filepath.Join(t.TempDir(), "salt")
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOrCreateSalt(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("salt mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestCredstoreKeyRequiresPassOrMachineKey(t *testing.T) {
	if _, err := deriveCredstoreKey(Config{XDGConfig: t.TempDir()}); err == nil {
		t.Fatal("deriveCredstoreKey accepted neither a passphrase nor a machine key")
	}
}

func TestCreateSessionGivesEngineTheToolCatalog(t *testing.T) {
	h := newShutdownTestHarness(t)
	defer h.Shutdown()
	sid, err := h.CreateSession(nil, "m")
	if err != nil {
		t.Fatal(err)
	}
	e := h.Mux.EngineFor(sid)
	if e == nil || len(e.Tools) == 0 || len(e.Tools) != len(h.toolSchemas()) {
		t.Fatal("the session's engine does not carry the registered tool schemas")
	}
}
