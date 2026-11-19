package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type tierBlindStore struct{ Store }

func (s tierBlindStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	q.Scoped, q.Tenanted = false, false
	return s.Store.ListTasks(ctx, owner, q)
}

func TestSweepPausedTrimStaysInTierOverBlindStore(t *testing.T) {
	h := tierHarness(t, tierBlindStore{NewMemoryStore()}, -1, 2)
	h.setHandler(func(_ context.Context, tc TaskContext) error { return tc.RequireInput(TextPart("more?")) })
	var kept []string
	for range 2 {
		kept = append(kept, h.send("alice@A").ID)
	}
	for range 3 {
		h.send("alice@B")
	}
	for _, id := range kept {
		if state, ok := h.taskState("alice@A", id); !ok || state != TaskStateInputRequired {
			t.Errorf("SECURITY: tenant A's paused task %s is %q (found=%v) after tenant B's activity", id, state, ok)
		}
	}
}

func TestSweepFirstAppendIsNotADelta(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{ArtifactID: "fresh", Parts: []Part{TextPart("one")}}, true); err != nil {
			return err
		}
		return tc.Complete()
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))
	r.nextResult(2 * time.Second)
	_, ev := r.nextResult(2 * time.Second)
	if ev.ArtifactUpdate == nil || ev.ArtifactUpdate.Append {
		t.Fatalf("an append to an artifact the client never saw = %+v, want append=false", ev)
	}
}

func TestSweepInternalHostNamesRefused(t *testing.T) {
	for _, raw := range []string{
		"https://localhost/hook",
		"https://api.localhost/hook",
		"https://db.corp.internal/hook",
		"https://metadata.google.internal/computeMetadata",
	} {
		if err := validatePushURL(raw, false, false); err == nil {
			t.Errorf("push URL %q accepted, want refused before any DNS lookup", raw)
		}
	}
}

func TestSweepTimestampKeepsZeroMillis(t *testing.T) {
	b, err := json.Marshal(Timestamp{time.Date(2026, 9, 1, 10, 0, 0, 0, time.FixedZone("x", 3600))})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `"2026-09-01T09:00:00.000Z"` {
		t.Fatalf("timestamp = %s, want UTC with three fraction digits", got)
	}
}

func TestSweepSuppliedClientNeverFollowsRedirects(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	p := newPusher(PushOptions{Client: &http.Client{}, AllowPrivate: true}, nil)
	if err := p.post(PushNotificationConfig{URL: redirect.URL, Token: "tok"}, []byte(`{}`)); err == nil {
		t.Fatal("a 302 from the receiver read as delivered")
	}
	if hits != 0 {
		t.Fatalf("the redirect was followed to a second host %d time(s)", hits)
	}
}
