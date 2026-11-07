package a2a

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// staleTrimStore is a custom store still on the pre-v0.96 trim method:
// it compiles, but no longer satisfies RetentionTrimmer.
type staleTrimStore struct{ Store }

func (staleTrimStore) TrimTerminalTasks(context.Context, string, int) error { return nil }

func (staleTrimStore) TrimPushConfigs(context.Context, string, string, int) error { return nil }

// pushOnlyTrimStore has the push trim but not the task trim.
type pushOnlyTrimStore struct{ Store }

func (pushOnlyTrimStore) TrimPushConfigs(context.Context, string, string, int) error { return nil }

func staleCfg(store Store) Config {
	return Config{
		Skills: []Skill{{ID: "echo", Name: "Echo", Handler: echoHandler}},
		Owner:  func(*http.Request) (string, bool) { return "u", true },
		Store:  store,
	}
}

func TestStaleTrimmerRefused(t *testing.T) {
	for name, store := range map[string]Store{
		"old TrimTerminalTasks": staleTrimStore{NewMemoryStore()},
		"push trim only":        pushOnlyTrimStore{NewMemoryStore()},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewServer(staleCfg(store))
			if err == nil {
				t.Fatal("NewServer accepted a store whose trim methods never run")
			}
			if !strings.Contains(err.Error(), "TrimTerminalTasks(ctx context.Context, owner string, tier RetentionTier, keep int) error") {
				t.Fatalf("error does not name the wanted signature: %v", err)
			}
		})
	}
}

func TestStaleTrimmerAllowedWhenRetentionOff(t *testing.T) {
	cfg := staleCfg(staleTrimStore{NewMemoryStore()})
	cfg.TerminalTaskRetention = -1
	cfg.MaxPushConfigsPerTask = -1
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer refused a store with retention turned off: %v", err)
	}
	_ = srv.Shutdown(context.Background())
}

// A store with no trim methods at all keeps its own retention policy.
type noTrimStore struct{ Store }

func TestNoTrimmerStoreAllowed(t *testing.T) {
	srv, err := NewServer(staleCfg(noTrimStore{NewMemoryStore()}))
	if err != nil {
		t.Fatalf("NewServer refused a store without trim methods: %v", err)
	}
	_ = srv.Shutdown(context.Background())
}

// Turning off task retention alone is not enough: the push-config bound
// still relies on RetentionTrimmer.
func TestStaleTrimmerRefusedWithPushBoundOn(t *testing.T) {
	cfg := staleCfg(staleTrimStore{NewMemoryStore()})
	cfg.TerminalTaskRetention = -1
	if _, err := NewServer(cfg); err == nil {
		t.Fatal("NewServer accepted a stale store while MaxPushConfigsPerTask is on")
	}
}

// A store wrapper that implements the current RetentionTrimmer passes.
type currentTrimStore struct{ *MemoryStore }

func TestCurrentTrimmerAllowed(t *testing.T) {
	srv, err := NewServer(staleCfg(currentTrimStore{NewMemoryStore()}))
	if err != nil {
		t.Fatalf("NewServer refused a current RetentionTrimmer: %v", err)
	}
	_ = srv.Shutdown(context.Background())
}
