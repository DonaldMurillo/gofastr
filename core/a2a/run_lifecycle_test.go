package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ownerRuns reads how many runs the registry counts against owner.
func ownerRuns(s *Server, owner string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runsByOwner[owner]
}

// listTasks returns the owner's tasks through ListTasks.
func (h *harness) listTasks(owner string) []Task {
	h.t.Helper()
	_, e, raw := h.call(owner, MethodListTasks, map[string]any{})
	if e.Error != nil {
		h.t.Fatalf("ListTasks: %s", raw)
	}
	var out ListTasksResponse
	if err := json.Unmarshal(e.Result, &out); err != nil {
		h.t.Fatalf("ListTasks result %s: %v", e.Result, err)
	}
	return out.Tasks
}

func streamRequest(t *testing.T, ctx context.Context, url, method string, params any) *http.Request {
	t.Helper()
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "call-1", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	return req
}

// failingFlusher is a streaming ResponseWriter whose every body write
// fails, as when the client is gone before the first event lands.
type failingFlusher struct{ h http.Header }

func (f *failingFlusher) Header() http.Header       { return f.h }
func (f *failingFlusher) WriteHeader(int)           {}
func (f *failingFlusher) Write([]byte) (int, error) { return 0, errors.New("client gone") }
func (f *failingFlusher) Flush()                    {}
func newFailingFlusher() *failingFlusher            { return &failingFlusher{h: http.Header{}} }

// TestStreamSeatRefusalNoStrandedRun pins that a SendStreamingMessage
// refused at the stream-seat cap leaves nothing behind: the refusal
// came after the task was created and its run claimed, so the task sat
// SUBMITTED forever and the owner's run quota leaked one slot per
// refusal.
func TestStreamSeatRefusalNoStrandedRun(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxStreamSeatsPerOwner = 1 })
	release := make(chan struct{})
	defer close(release)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		<-release
		return tc.Complete(TextPart("done"))
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))
	r.nextResult(2 * time.Second) // the first stream holds the only seat

	status, _, raw := h.call("alice", MethodSendStreamingMessage, streamSendParams(""))
	if status != http.StatusTooManyRequests {
		t.Fatalf("second stream: status %d, want 429 (%s)", status, raw)
	}
	if n := ownerRuns(h.srv, "alice"); n != 1 {
		t.Errorf("owner run count after the refusal = %d, want 1 (the refused stream's run leaked)", n)
	}
	if tasks := h.listTasks("alice"); len(tasks) != 1 {
		t.Errorf("owner has %d tasks after the refusal, want 1 (the refused stream left a task behind)", len(tasks))
	}
}

// TestStreamSnapshotFailNoStrandedRun pins that a stream whose first
// snapshot write fails still accounts for the task it created: the run
// starts (exactly as for a client that leaves right after the
// snapshot), finishes, and releases its registration.
func TestStreamSnapshotFailNoStrandedRun(t *testing.T) {
	h := newHarness(t, nil)
	h.srv.ServeHTTP(newFailingFlusher(), streamRequest(t, context.Background(), h.ts.URL, MethodSendStreamingMessage, streamSendParams("")))

	deadline := time.Now().Add(2 * time.Second)
	for ownerRuns(h.srv, "alice") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("run still registered after the failed snapshot write")
		}
		time.Sleep(5 * time.Millisecond)
	}
	tasks := h.listTasks("alice")
	if len(tasks) != 1 || !tasks[0].Status.State.Terminal() {
		t.Fatalf("tasks after the failed snapshot = %+v, want one terminal task", tasks)
	}
}

// gateFlusher captures the stream and parks the FIRST flush until
// proceed closes, holding the stream between "snapshot sent" and
// whatever streamSend does next.
type gateFlusher struct {
	h       http.Header
	mu      sync.Mutex
	buf     bytes.Buffer
	once    sync.Once
	flushed chan struct{}
	proceed chan struct{}
}

func (g *gateFlusher) Header() http.Header { return g.h }
func (g *gateFlusher) WriteHeader(int)     {}
func (g *gateFlusher) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Write(p)
}

func (g *gateFlusher) Flush() {
	g.once.Do(func() {
		close(g.flushed)
		<-g.proceed
	})
}

func (g *gateFlusher) firstEvent(t *testing.T) StreamResponse {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	sc := bufio.NewScanner(bytes.NewReader(g.buf.Bytes()))
	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var e env
			var sr StreamResponse
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("event %q: %v", line, err)
			}
			if err := json.Unmarshal(e.Result, &sr); err != nil {
				t.Fatalf("event result %s: %v", e.Result, err)
			}
			return sr
		}
	}
	t.Fatalf("no event in %q", g.buf.String())
	return StreamResponse{}
}

// TestCancelBeforeStartRunSkipsHandler pins that a CancelTask landing
// after the streaming snapshot (the client now knows the id) but before
// the run starts is not lost: the claim's placeholder cancel was a
// no-op, so the handler then ran for a task already CANCELED.
func TestCancelBeforeStartRunSkipsHandler(t *testing.T) {
	h := newHarness(t, nil)
	var invoked atomic.Int64
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		invoked.Add(1)
		return tc.Complete(TextPart("done"))
	})
	g := &gateFlusher{h: http.Header{}, flushed: make(chan struct{}), proceed: make(chan struct{})}
	served := make(chan struct{})
	go func() {
		defer close(served)
		h.srv.ServeHTTP(g, streamRequest(t, context.Background(), h.ts.URL, MethodSendStreamingMessage, streamSendParams("")))
	}()
	select {
	case <-g.flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot never flushed")
	}
	snap := g.firstEvent(t)
	if snap.Task == nil {
		t.Fatalf("first event is not the task snapshot: %+v", snap)
	}
	_, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": snap.Task.ID})
	if e.Error != nil {
		t.Fatalf("CancelTask: %s", raw)
	}
	close(g.proceed)
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end after the canceled run")
	}
	if n := invoked.Load(); n != 0 {
		t.Errorf("handler ran %d times for a task canceled before its run started, want 0", n)
	}
	if got := h.waitTask("alice", snap.Task.ID, TaskStateCanceled, 2*time.Second); got.Status.State != TaskStateCanceled {
		t.Errorf("state = %s, want CANCELED", got.Status.State)
	}
	if n := ownerRuns(h.srv, "alice"); n != 0 {
		t.Errorf("run still registered: %d", n)
	}
}

// A CancelTask that lands while the snapshot is still being written
// reaches the stream: the stream ends on CANCELED, not on EOF after
// SUBMITTED. The cancel published before the stream had subscribed, and
// the run, finding the task already canceled, published nothing more.
func TestCancelDuringSnapshotReachesStream(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.Complete(TextPart("done"))
	})
	g := &gateFlusher{h: http.Header{}, flushed: make(chan struct{}), proceed: make(chan struct{})}
	served := make(chan struct{})
	go func() {
		defer close(served)
		h.srv.ServeHTTP(g, streamRequest(t, context.Background(), h.ts.URL, MethodSendStreamingMessage, streamSendParams("")))
	}()
	select {
	case <-g.flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot never flushed")
	}
	snap := g.firstEvent(t)
	if _, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": snap.Task.ID}); e.Error != nil {
		t.Fatalf("CancelTask: %s", raw)
	}
	close(g.proceed)
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end after the canceled run")
	}
	evs := g.events(t)
	last := evs[len(evs)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != TaskStateCanceled {
		t.Errorf("last stream event = %+v, want a CANCELED status update", last)
	}
}

// events decodes every SSE event written so far.
func (g *gateFlusher) events(t *testing.T) []StreamResponse {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []StreamResponse
	sc := bufio.NewScanner(bytes.NewReader(g.buf.Bytes()))
	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var e env
			var sr StreamResponse
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("event %q: %v", line, err)
			}
			if err := json.Unmarshal(e.Result, &sr); err != nil {
				t.Fatalf("event result %s: %v", e.Result, err)
			}
			out = append(out, sr)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no event in %q", g.buf.String())
	}
	return out
}

// gatedGetStore parks the first GetTask after arm until release closes.
type gatedGetStore struct {
	Store
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (g *gatedGetStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if g.armed.CompareAndSwap(true, false) {
		close(g.entered)
		<-g.release
	}
	return g.Store.GetTask(ctx, owner, id)
}

func partTexts(parts []Part) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Text != nil {
			out = append(out, *p.Text)
		}
	}
	return out
}

// TestResubscribeNoDuplicateAppend pins that SubscribeToTask never
// forwards an event the snapshot already reflects. It subscribes to the
// bus before reading the snapshot, so an artifact append published in
// that gap is in the snapshot AND queued as an append=true delta; a
// client applying deltas to the snapshot doubled those parts.
func TestResubscribeNoDuplicateAppend(t *testing.T) {
	gs := &gatedGetStore{Store: NewMemoryStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := newHarness(t, func(c *Config) { c.Store = gs })
	firstDone, appendNow, appended, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{ArtifactID: "a", Parts: []Part{TextPart("one")}}, false); err != nil {
			return err
		}
		close(firstDone)
		<-appendNow
		if err := tc.Artifact(Artifact{ArtifactID: "a", Parts: []Part{TextPart("two")}}, true); err != nil {
			return err
		}
		close(appended)
		<-finish
		return tc.Complete()
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	<-firstDone

	gs.armed.Store(true)
	streams := make(chan *sseReader, 1)
	go func() { streams <- h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID}) }()
	select {
	case <-gs.entered: // subscribed to the bus, snapshot not yet read
	case <-time.After(2 * time.Second):
		t.Fatal("subscribe never read the snapshot")
	}
	close(appendNow)
	<-appended
	close(gs.release)
	r := <-streams

	_, sr := r.nextResult(2 * time.Second)
	if sr.Task == nil || len(sr.Task.Artifacts) != 1 {
		t.Fatalf("snapshot = %+v, want the task with one artifact", sr)
	}
	parts := partTexts(sr.Task.Artifacts[0].Parts)
	close(finish)
	for {
		_, ev := r.nextResult(2 * time.Second)
		if ev.ArtifactUpdate != nil {
			if ev.ArtifactUpdate.Append {
				parts = append(parts, partTexts(ev.ArtifactUpdate.Artifact.Parts)...)
			} else {
				parts = partTexts(ev.ArtifactUpdate.Artifact.Parts)
			}
		}
		if endsStream(ev) {
			break
		}
	}
	if strings.Join(parts, ",") != "one,two" {
		t.Errorf("client-side artifact after applying the stream = %v, want [one two]", parts)
	}
}

// A subscribe that took the bus of a run which then settled, while a
// resume started the next run before the snapshot read, follows the new
// run to its end. It forwarded the old run's closed bus, so the stream
// ended after a WORKING snapshot and the completion never arrived.
func TestResubscribeFollowsNewerRun(t *testing.T) {
	gs := &gatedGetStore{Store: NewMemoryStore(), entered: make(chan struct{}), release: make(chan struct{})}
	h := newHarness(t, func(c *Config) { c.Store = gs })
	r1Started, r1Go, r2Started, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if runs.Add(1) == 1 {
			close(r1Started)
			<-r1Go
			return tc.RequireInput(TextPart("need input"))
		}
		close(r2Started)
		<-finish
		return tc.Complete(TextPart("done"))
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	<-r1Started

	gs.armed.Store(true)
	streams := make(chan *sseReader, 1)
	go func() { streams <- h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID}) }()
	select {
	case <-gs.entered: // holding run 1's bus, snapshot not yet read
	case <-time.After(2 * time.Second):
		t.Fatal("subscribe never read the snapshot")
	}
	close(r1Go)
	for deadline := time.Now().Add(2 * time.Second); h.srv.runFor(task.ID) != nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("run 1 never settled")
		}
	}
	if status, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message":       map[string]any{"taskId": task.ID, "role": "ROLE_USER", "parts": []any{map[string]any{"text": "answer"}}},
		"configuration": map[string]any{"returnImmediately": true},
	}); status != 200 || e.Error != nil {
		t.Fatalf("resume: status=%d body=%s", status, raw)
	}
	<-r2Started
	close(gs.release)
	r := <-streams

	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil || sr.Task.Status.State.Terminal() {
		t.Fatalf("snapshot = %+v, want the running task", sr)
	}
	close(finish)
	for {
		_, ev := r.nextResult(2 * time.Second)
		if endsStream(ev) {
			if ev.StatusUpdate == nil || ev.StatusUpdate.Status.State != TaskStateCompleted {
				t.Fatalf("stream ended on %+v, want the COMPLETED update", ev)
			}
			return
		}
	}
}

// hungGetStore makes every GetTask after arm block until its context
// ends, as a wedged database does.
type hungGetStore struct {
	Store
	hang      atomic.Bool
	entered   chan struct{}
	unblocked chan struct{}
	// stop frees a read the context never reached, so a failing test
	// still lets the httptest server close.
	stop      chan struct{}
	enterOnce sync.Once
	doneOnce  sync.Once
}

func (s *hungGetStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if s.hang.Load() {
		s.enterOnce.Do(func() { close(s.entered) })
		select {
		case <-ctx.Done():
			s.doneOnce.Do(func() { close(s.unblocked) })
			return nil, ctx.Err()
		case <-s.stop:
			return nil, errors.New("test over")
		}
	}
	return s.Store.GetTask(ctx, owner, id)
}

func newHungGetStore() *hungGetStore {
	return &hungGetStore{Store: NewMemoryStore(), entered: make(chan struct{}), unblocked: make(chan struct{}), stop: make(chan struct{})}
}

// TestSubscribeStoreReadsHonorRequestContext pins that the
// SubscribeToTask store reads (the one before the snapshot, and the
// polling fallback's) run under the request context: under
// context.Background a hung store pinned the handler, and with it the
// stream seat, after the client left.
func TestSubscribeStoreReadsHonorRequestContext(t *testing.T) {
	t.Run("snapshot read", func(t *testing.T) {
		hs := newHungGetStore()
		h := newHarness(t, func(c *Config) { c.Store = hs })
		t.Cleanup(func() { close(hs.stop) }) // runs before the server closes
		task := h.send("alice")
		hs.hang.Store(true)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			resp, err := http.DefaultClient.Do(streamRequest(t, ctx, h.ts.URL, MethodSubscribeToTask, map[string]any{"id": task.ID}))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		select {
		case <-hs.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("subscribe never read the store")
		}
		cancel()
		select {
		case <-hs.unblocked:
		case <-time.After(2 * time.Second):
			t.Fatal("the snapshot read ignored the client leaving")
		}
	})
	t.Run("poll read", func(t *testing.T) {
		hs := newHungGetStore()
		h := newHarness(t, func(c *Config) { c.Store = hs })
		t.Cleanup(func() { close(hs.stop) }) // runs before the server closes
		h.srv.pollEvery = 5 * time.Millisecond
		// A WORKING task with no run in this process: the
		// multi-replica polling fallback serves it.
		if err := hs.Store.CreateTask(context.Background(), rec("alice", "remote-1", string(TaskStateWorking), t0)); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		resp, err := http.DefaultClient.Do(streamRequest(t, ctx, h.ts.URL, MethodSubscribeToTask, map[string]any{"id": "remote-1"}))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content type %q, want text/event-stream", ct)
		}
		hs.hang.Store(true)
		select {
		case <-hs.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("poll never read the store")
		}
		cancel()
		select {
		case <-hs.unblocked:
		case <-time.After(2 * time.Second):
			t.Fatal("the poll read ignored the client leaving")
		}
	})
}

// TestPausedTasksBounded: a skill that pauses on every send minted one
// row per send that no retention touched. Past PausedTaskRetention the
// oldest paused tasks are canceled, which hands them to terminal
// retention.
func TestPausedTasksBounded(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.PausedTaskRetention = 2 })
	var tick atomic.Int64
	h.srv.now = func() time.Time { return t0.Add(time.Duration(tick.Add(1)) * time.Millisecond) }
	// Both paused states, alternating, so the bound counts their merge.
	var sends atomic.Int64
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if sends.Add(1)%2 == 0 {
			return tc.RequireAuth(TextPart("sign in"))
		}
		return tc.RequireInput(TextPart("more?"))
	})
	var ids []string
	for range 4 {
		ids = append(ids, h.send("alice").ID)
	}
	h.waitTask("alice", ids[0], TaskStateCanceled, time.Second)
	h.waitTask("alice", ids[1], TaskStateCanceled, time.Second)
	h.waitTask("alice", ids[2], TaskStateInputRequired, time.Second)
	h.waitTask("alice", ids[3], TaskStateAuthRequired, time.Second)
}

// movingPausedStore answers the AUTH_REQUIRED query with every
// INPUT_REQUIRED task too, as if each moved between the two queries.
type movingPausedStore struct{ Store }

func (m *movingPausedStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	recs, next, err := m.Store.ListTasks(ctx, owner, q)
	if err != nil || q.Status != TaskStateAuthRequired {
		return recs, next, err
	}
	q.Status = TaskStateInputRequired
	moved, _, err := m.Store.ListTasks(ctx, owner, q)
	for _, rec := range moved {
		c := rec.Clone()
		c.Task.Status.State = TaskStateAuthRequired
		recs = append(recs, c)
	}
	return recs, next, err
}

// TestPausedTaskMovingCountsOnce: a task that moves between the two
// paused states while the trim lists them is one task, not two.
func TestPausedTaskMovingCountsOnce(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.PausedTaskRetention = 3
		c.Store = &movingPausedStore{Store: NewMemoryStore()}
	})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("more?"))
	})
	var ids []string
	for range 3 {
		ids = append(ids, h.send("alice").ID)
	}
	for _, id := range ids {
		h.waitTask("alice", id, TaskStateInputRequired, time.Second)
	}
}

// TestInvalidSkillPartRefused: a part with no content, DataPart(nil)
// included, is refused at the call. Stored, it made the task undecodable
// for every later read.
func TestInvalidSkillPartRefused(t *testing.T) {
	h := newHarness(t, nil)
	got := make(chan []error, 1)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		got <- []error{
			tc.Working(Part{}),
			tc.Artifact(Artifact{Parts: []Part{DataPart(nil)}}, false),
			tc.RequireInput(DataPart(nil)),
			tc.Complete(DataPart(nil)),
			// Typed nils and a raw null encode as {"data":null} too.
			tc.Working(DataPart(map[string]any(nil))),
			tc.Working(DataPart([]any(nil))),
			tc.Working(DataPart((*struct{})(nil))),
			tc.Working(DataPart(json.RawMessage(" null"))),
		}
		return tc.Complete(TextPart("done"))
	})
	task := h.send("alice")
	for i, err := range <-got {
		if err == nil {
			t.Errorf("call %d accepted a part with no content", i)
		}
	}
	if task.Status.State != TaskStateCompleted || len(task.Artifacts) != 0 {
		t.Fatalf("task = %s with %d artifacts, want COMPLETED with none", task.Status.State, len(task.Artifacts))
	}
	h.waitTask("alice", task.ID, TaskStateCompleted, time.Second)
}
