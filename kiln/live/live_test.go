package live_test

import (
	"bufio"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	"github.com/DonaldMurillo/gofastr/framework"
)

// --- helpers ----------------------------------------------------------

func newTestLive(t *testing.T, j journal.Journal) (*live.Live, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	factory := func() *framework.App {
		return framework.NewApp(framework.WithDB(db))
	}
	l, err := live.New(j, factory)
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	return l, db
}

func newEntry(t *testing.T, id string, ts time.Time, kind journal.Kind, op journal.Op, payload any) journal.Entry {
	t.Helper()
	e, err := journal.NewEntry(id, ts, kind, op, payload)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

// --- Mutator tests ----------------------------------------------------

func TestNewLiveReplaysJournal(t *testing.T) {
	j := journal.NewMemory()
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if _, err := j.Append(newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatal(err)
	}

	l, _ := newTestLive(t, j)
	if _, ok := l.Session().World.Entities["posts"]; !ok {
		t.Fatal("expected posts entity from replay")
	}
}

func TestApplyAddEntityPersistsAndRebuilds(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	entry := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})

	if err := l.Apply(entry); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Session reflects the change.
	if _, ok := l.Session().World.Entities["posts"]; !ok {
		t.Error("session missing posts after Apply")
	}
	// Journal has the entry.
	if n, _ := l.Journal().Len(); n != 1 {
		t.Errorf("journal len = %d, want 1", n)
	}
	// New app handles the route.
	req := httptest.NewRequest(http.MethodGet, "/posts", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Errorf("/posts should be registered after Apply, got 404")
	}
}

func TestApplyInvalidEntryDoesNotPersist(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	first := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})
	if err := l.Apply(first); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}

	// Duplicate add — should fail.
	dup := newEntry(t, "2", time.Now(), journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})
	if err := l.Apply(dup); err == nil {
		t.Fatal("duplicate add_entity should error")
	}

	if n, _ := l.Journal().Len(); n != 1 {
		t.Errorf("journal len = %d after rejected entry, want 1", n)
	}
}

func TestReloadRecoversFromJournal(t *testing.T) {
	j := journal.NewMemory()
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if _, err := j.Append(newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatal(err)
	}
	l, _ := newTestLive(t, j)
	// Append directly to the journal (simulating an external write or restart).
	pages := &world.Page{Path: "/x", Type: "page", Tree: world.Node{Kind: "div"}}
	if _, err := j.Append(newEntry(t, "2", time.Now(), journal.KindWorldEdit, journal.OpAddPage,
		journal.AddPagePayload{Page: pages})); err != nil {
		t.Fatal(err)
	}
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, ok := l.Session().World.Pages["/x"]; !ok {
		t.Error("Reload should pick up out-of-band journal entries")
	}
}

// --- SSE / Broadcaster tests ------------------------------------------

func TestApplyBroadcastsEvent(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	ch, unsub := l.Subscribe()
	defer unsub()

	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "body", Type: "string"}}}
	entry := newEntry(t, "e1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})

	if err := l.Apply(entry); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.EntryID != "e1" {
			t.Errorf("event EntryID = %q, want e1", ev.EntryID)
		}
		if ev.Kind != string(journal.KindWorldEdit) {
			t.Errorf("event Kind = %q, want world_edit", ev.Kind)
		}
		if ev.Summary != "name=posts fields=2" {
			t.Errorf("event Summary = %q, want %q", ev.Summary, "name=posts fields=2")
		}
	case <-time.After(time.Second):
		t.Fatal("no SSE event after Apply")
	}
}

func TestSSEHandlerStreamsEvents(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	srv := httptest.NewServer(http.HandlerFunc(l.ServeSSE))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Per-test client whose transport closes idle conns at cleanup. The
	// process-wide http.DefaultClient pools per-Transport, so connections
	// from finished tests stay open until the keep-alive timer fires —
	// under parallel package execution that burns the macOS 49152-65535
	// ephemeral range faster than TIME_WAIT clears (15s), and outbound
	// dials surface as "can't assign requested address".
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/.kiln/events", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	// Apply an edit; the streamed body should contain it.
	var wg sync.WaitGroup
	wg.Add(1)
	got := make(chan string, 1)
	go func() {
		defer wg.Done()
		buf := bufio.NewReader(resp.Body)
		// Read up to 2KB or 2s, whichever comes first.
		out := make([]byte, 0, 2048)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && len(out) < 2048 {
			b, err := buf.ReadByte()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}
			out = append(out, b)
			if strings.Contains(string(out), "\"entry_id\":\"e1\"") {
				break
			}
		}
		got <- string(out)
	}()

	// Brief wait so the subscriber is registered before we Apply.
	time.Sleep(50 * time.Millisecond)
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if err := l.Apply(newEntry(t, "e1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	select {
	case body := <-got:
		if !strings.Contains(body, "e1") {
			t.Errorf("SSE body did not contain e1: %q", body)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("SSE handler did not stream event")
	}
	cancel()
	wg.Wait()
}

func TestBroadcasterMultipleSubscribers(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	a, unsubA := l.Subscribe()
	b, unsubB := l.Subscribe()
	defer unsubA()
	defer unsubB()

	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if err := l.Apply(newEntry(t, "e1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for name, ch := range map[string]<-chan live.Event{"a": a, "b": b} {
		select {
		case ev := <-ch:
			if ev.EntryID != "e1" {
				t.Errorf("subscriber %s got %v, want e1", name, ev)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %s timed out", name)
		}
	}
}
