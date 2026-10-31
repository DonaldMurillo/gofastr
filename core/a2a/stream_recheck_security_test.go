package a2a

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// revocableOwner resolves "bob" until revoked, then refuses him, or,
// with moved set, resolves the same credentials to "carol".
func revocableOwner(revoked, moved *atomic.Bool) func(*http.Request) (string, bool) {
	return func(r *http.Request) (string, bool) {
		o := r.Header.Get("X-Owner")
		if o == "" {
			o = "alice"
		}
		if o == "bob" && revoked.Load() {
			if moved.Load() {
				return "carol", true
			}
			return "", false
		}
		return o, true
	}
}

// A task stream resolved its owner once, at connect. A caller revoked after
// that kept receiving the task's events until it disconnected (2026-10-27
// bug bash, unrechecked). The keep-alive re-runs Owner and ends the stream.
func TestStreamEndsWhenOwnerRevoked(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(map[bool]string{false: "refused", true: "resolves elsewhere"}[move], func(t *testing.T) {
			testStreamEndsWhenOwnerRevoked(t, move)
		})
	}
}

func testStreamEndsWhenOwnerRevoked(t *testing.T, move bool) {
	var revoked, moved atomic.Bool
	moved.Store(move)
	h := newHarness(t, func(c *Config) { c.Owner = revocableOwner(&revoked, &moved) })
	release := make(chan struct{})
	defer close(release)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	r := h.openStream("bob", MethodSendStreamingMessage, streamSendParams(""))
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no task snapshot: %+v", sr)
	}
	revoked.Store(true)
	r.eof(2 * time.Second)
}

// The cross-replica fallback polls the store; it re-checks the owner too.
func TestPollStreamEndsWhenOwnerRevoked(t *testing.T) {
	db := openSQLite(t)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	var revoked, moved atomic.Bool
	newReplica := func() *harness {
		var h *harness
		h = newHarness(t, func(c *Config) {
			c.Store = store
			c.Owner = revocableOwner(&revoked, &moved)
			c.Skills = []Skill{{
				ID: "echo", Name: "Echo", Description: "echo",
				Handler: func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
			}}
		})
		h.srv.pollEvery = 20 * time.Millisecond
		return h
	}
	a, b := newReplica(), newReplica()
	release := make(chan struct{})
	defer close(release)
	a.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-release
		return nil
	})
	task := a.send("bob", map[string]any{"returnImmediately": true})
	a.waitTask("bob", task.ID, TaskStateWorking, 2*time.Second)

	r := b.openStream("bob", MethodSubscribeToTask, map[string]any{"id": task.ID})
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no snapshot on B: %+v", sr)
	}
	revoked.Store(true)
	r.eof(2 * time.Second)
}
