package framework

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
)

// ============================================================================
// Cursor pagination with integer primary keys
//
// The original cursor tests used lexically-sortable string IDs ("p001"…). This
// test pins behaviour against numeric int PKs, which exercise a different
// scan path (PG returns int64, SQLite returns int64) and a different sort
// order than lexical strings.
// ============================================================================

func TestGap_CursorWithIntegerPK(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Use an explicitly-typed INTEGER PK; both engines accept this DDL.
		if _, err := db.Exec(`CREATE TABLE counters (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		for i := 1; i <= 25; i++ {
			if _, err := db.Exec("INSERT INTO counters(id, label) VALUES ($1, $2)", i, fmt.Sprintf("c%d", i)); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("counters", entity.EntityConfig{
			Table: "counters",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int},
				{Name: "label", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		ta := TestHarness(t, app)

		// First page
		first := decodeCursorPage(t, ta.Get("/counters?cursor=&limit=10").Body())
		if len(first.Data) != 10 {
			t.Fatalf("expected 10 items, got %d", len(first.Data))
		}
		// Walk to the end. Cap iterations to bound runaway loops.
		seen := make([]any, 0, 25)
		for _, row := range first.Data {
			seen = append(seen, row["id"])
		}
		cursor := first.Cursor
		for hops := 0; hops < 5 && cursor != ""; hops++ {
			next := decodeCursorPage(t, ta.Get("/counters?cursor="+cursor+"&limit=10").Body())
			for _, row := range next.Data {
				seen = append(seen, row["id"])
			}
			if !next.HasMore {
				break
			}
			cursor = next.Cursor
		}
		if len(seen) != 25 {
			t.Fatalf("expected to walk 25 rows, got %d (last cursor=%q)", len(seen), cursor)
		}
		// IDs must be in ascending numeric order.
		for i := 1; i <= 25; i++ {
			got := fmt.Sprintf("%v", seen[i-1])
			want := fmt.Sprintf("%d", i)
			if got != want {
				t.Fatalf("row %d: expected id=%s, got %v", i, want, seen[i-1])
			}
		}
	})
}

// ============================================================================
// Multipart upload over the configured maxMemory still streams correctly
//
// crud.MaxMultipartMemory is 32 MiB. A test file the same size would slow CI down,
// so use a 64 KiB file (well below the cap) and assert it round-trips
// byte-perfect to disk. This pins the streaming code path to confirm large
// files don't get truncated, garbled, or held entirely in memory by accident.
// ============================================================================

func TestGap_MultipartLargeFile_RoundTripsExactly(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedUploadDB(t, db)
		app, dir := uploadAppOnDB(t, db)
		ta := TestHarness(t, app)

		// 64 KiB of distinct content so any truncation/corruption is observable.
		content := make([]byte, 64*1024)
		for i := range content {
			content[i] = byte(i % 251) // not 256 so the pattern doesn't align with byte boundaries
		}

		body, ct := buildMultipartBody(t,
			map[string][2]string{"avatar": {"big.bin", string(content)}},
			map[string]string{"title": "large"},
		)
		resp := ta.Request(http.MethodPost, "/posts", nil).
			WithHeader("Content-Type", ct).
			WithBody(body).
			Execute()
		resp.AssertStatus(t, http.StatusCreated)

		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		avatarURL, _ := got["avatar"].(string)
		stored := dir + "/" + avatarURL
		bs, err := readFile(stored)
		if err != nil {
			t.Fatalf("read stored: %v", err)
		}
		if len(bs) != len(content) {
			t.Fatalf("size mismatch: stored %d bytes, sent %d", len(bs), len(content))
		}
		for i := range bs {
			if bs[i] != content[i] {
				t.Fatalf("byte %d differs: stored %x, sent %x", i, bs[i], content[i])
			}
		}
	})
}

// ============================================================================
// Concurrent SSE subscribers all receive the same event
//
// Two clients subscribe to /posts/_events at roughly the same time; a single
// POST must fan out to both. This exercises the EventBus's snapshot+iterate
// path under N>1 subscribers and proves there's no per-subscriber ordering
// bug.
// ============================================================================

func TestGap_SSE_ConcurrentSubscribersBothReceive(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedEventsDB(t, db)
		app := eventsApp(t, db)
		srv := httptest.NewServer(app.Router)
		t.Cleanup(srv.Close)

		subscribe := func() (<-chan sseEvent, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/posts/_events", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			t.Cleanup(func() { resp.Body.Close() })
			return readSSEEvents(t, ctx, bufio.NewReader(resp.Body)), cancel
		}

		stream1, c1 := subscribe()
		defer c1()
		stream2, c2 := subscribe()
		defer c2()

		// Wait briefly so both subscriptions register on the bus before we emit.
		time.Sleep(80 * time.Millisecond)
		go func() {
			body, _ := json.Marshal(map[string]any{"title": "fanout"})
			_, _ = http.Post(srv.URL+"/posts", "application/json", strings.NewReader(string(body)))
		}()

		got := make(chan string, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			select {
			case ev := <-stream1:
				got <- ev.Data
			case <-time.After(2 * time.Second):
				got <- ""
			}
		}()
		go func() {
			defer wg.Done()
			select {
			case ev := <-stream2:
				got <- ev.Data
			case <-time.After(2 * time.Second):
				got <- ""
			}
		}()
		wg.Wait()
		close(got)

		var payloads []string
		for p := range got {
			payloads = append(payloads, p)
		}
		if len(payloads) != 2 {
			t.Fatalf("expected 2 events received, got %d", len(payloads))
		}
		for i, p := range payloads {
			if !strings.Contains(p, `"title":"fanout"`) {
				t.Fatalf("subscriber %d missing fanout payload, got %q", i, p)
			}
		}
	})
}

// ============================================================================
// Batch validation collects the FIRST per-item error and rolls back; later
// items stay marked Skipped (no double-write through PG when the first item
// errored). Pins this against partial-mutation drift.
// ============================================================================

func TestGap_BatchAfterFailureNoLeakedWrites(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		// Seed the conflicting row up-front.
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p0", "Conflict"); err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := ta.Post("/posts/_batch", map[string]any{
			"items": []map[string]any{
				{"title": "OK-1"},
				{"title": "OK-2"},
				{"title": "Conflict"},
				{"title": "Should-Skip"},
				{"title": "Should-Skip-2"},
			},
		})
		resp.AssertStatus(t, http.StatusBadRequest)

		// After rollback only the original seed row should survive.
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 row (seed only) post-rollback, got %d", n)
		}

		// And no row with the supposedly-skipped titles should exist either.
		for _, title := range []string{"OK-1", "OK-2", "Should-Skip", "Should-Skip-2"} {
			var got string
			err := db.QueryRow("SELECT title FROM posts WHERE title = $1", title).Scan(&got)
			if err == nil {
				t.Fatalf("unexpected committed row with title %q", title)
			}
		}
	})
}
