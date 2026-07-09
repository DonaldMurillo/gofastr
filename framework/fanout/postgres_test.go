package fanout

// Real-Postgres coverage for the LISTEN/NOTIFY fanout: inline delivery,
// table-fallback for >8KB payloads, and opportunistic purge. Skips
// automatically when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/lib/pq"
)

// pgFanout provisions a fresh live-Postgres database (skipping when none is
// reachable) and returns a PostgresFanout over it plus the raw DB. The fanout
// creates the fallback table.
func pgFanout(t *testing.T, opts ...Option) (*PostgresFanout, *sql.DB) {
	t.Helper()
	dsn := pgtest.FreshDatabaseDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	f, err := NewPostgres(dsn, db, opts...)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, db
}

// recv collects payloads under a mutex and waits for N to arrive.
type recv struct {
	mu  sync.Mutex
	got []string
}

func (r *recv) add(p []byte) {
	r.mu.Lock()
	r.got = append(r.got, string(p))
	r.mu.Unlock()
}

func (r *recv) waitN(t *testing.T, want int, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.got)
		r.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("%s: wanted %d, got %d", msg, want, len(r.got))
}

// TestPostgres_TableCreated: construction creates the fallback table.
func TestPostgres_TableCreated(t *testing.T) {
	_, db := pgFanout(t)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, "gofastr_fanout_msgs").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("fallback table not created (count=%d)", n)
	}
}

// TestPostgres_PublishSubscribeRoundTrip: a small inline message round-trips.
func TestPostgres_PublishSubscribeRoundTrip(t *testing.T) {
	f, _ := pgFanout(t)
	r := &recv{}
	cancel, err := f.Subscribe("topicA", r.add)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if err := f.Publish(context.Background(), "topicA", []byte("hello-pg")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	r.waitN(t, 1, "inline round-trip")
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.got[0] != "hello-pg" {
		t.Errorf("payload = %q, want hello-pg", r.got[0])
	}
}

// TestPostgres_TopicIsolation: a message on topicB does not reach topicA.
func TestPostgres_TopicIsolation(t *testing.T) {
	f, _ := pgFanout(t)
	var aCount atomic.Int64
	f.Subscribe("isoA", func([]byte) { aCount.Add(1) })
	f.Subscribe("isoB", func([]byte) {})

	if err := f.Publish(context.Background(), "isoB", []byte("b-only")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && aCount.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if aCount.Load() != 0 {
		t.Fatalf("topicA received a message published to topicB")
	}
}

// TestPostgres_BigPayloadFallsBackToTable: a >8KB payload round-trips via the
// table fallback path.
func TestPostgres_BigPayloadFallsBackToTable(t *testing.T) {
	f, _ := pgFanout(t)
	big := strings.Repeat("x", 9000) // over the ~7000 inline threshold
	r := &recv{}
	cancel, err := f.Subscribe("big", r.add)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if err := f.Publish(context.Background(), "big", []byte(big)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	r.waitN(t, 1, "big payload table fallback")
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got[0]) != 9000 {
		t.Errorf("payload len = %d, want 9000", len(r.got[0]))
	}
}

// TestPostgres_CancelStopsDelivery: after cancel, no more deliveries.
func TestPostgres_CancelStopsDelivery(t *testing.T) {
	f, _ := pgFanout(t)
	var n atomic.Int64
	cancel, _ := f.Subscribe("cx", func([]byte) { n.Add(1) })
	cancel()

	if err := f.Publish(context.Background(), "cx", []byte("after")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) && n.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() != 0 {
		t.Fatalf("received after cancel: %d", n.Load())
	}
}

// TestPostgres_PurgeRemovesOldRows: rows older than the purge age are removed
// by an opportunistic purge triggered on a table-fallback publish.
func TestPostgres_PurgeRemovesOldRows(t *testing.T) {
	f, db := pgFanout(t)

	// Insert one row back-dated well past the purge age.
	old := time.Now().Add(-10 * time.Minute)
	if _, err := db.Exec(`INSERT INTO gofastr_fanout_msgs (payload, created_at) VALUES ($1, $2)`, "stale-old", old); err != nil {
		t.Fatalf("insert: %v", err)
	}

	count := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM gofastr_fanout_msgs`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if got := count(); got != 1 {
		t.Fatalf("after insert, count = %d, want 1", got)
	}

	// A big publish triggers the table path, which (a) inserts its own
	// fallback row and (b) opportunistically purges stale rows. After it,
	// the old back-dated row must be gone while the new fallback row stays.
	cancel, _ := f.Subscribe("purge", func([]byte) {})
	defer cancel()
	if err := f.Publish(context.Background(), "purge", []byte(strings.Repeat("y", 9000))); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Purge runs synchronously inside publishViaTable; allow a little room.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := count(); got != 1 {
		t.Fatalf("after purge, count = %d, want 1 (old row not removed, new fallback expected)", got)
	}
	// The surviving row must be the fresh fallback, not the stale one.
	var payload string
	if err := db.QueryRow(`SELECT payload FROM gofastr_fanout_msgs`).Scan(&payload); err != nil {
		t.Fatalf("select survivor: %v", err)
	}
	if payload == "stale-old" {
		t.Fatalf("stale row survived purge; payload = %q", payload)
	}
}

// TestPostgres_WithoutEnsureTable: the option skips table creation.
func TestPostgres_WithoutEnsureTable(t *testing.T) {
	_, db := pgFanout(t, WithoutEnsureTable())
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, "gofastr_fanout_msgs").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("WithoutEnsureTable should have skipped creation (count=%d)", n)
	}
}

// TestPostgres_BadDSNTimeout: an unroutable DSN makes pq.Listener retry
// forever; WithListenTimeout must turn that into a prompt construction error
// instead of an indefinite hang (no live Postgres required).
func TestPostgres_BadDSNTimeout(t *testing.T) {
	// sql.Open does not connect; WithoutEnsureTable skips CREATE TABLE so the
	// only network touch is the LISTEN listener against an unroutable port.
	db, err := sql.Open("postgres", "user=postgres dbname=unused")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	start := time.Now()
	_, err = NewPostgres(
		"host=127.0.0.1 port=1 sslmode=disable user=postgres dbname=unused", // unroutable port
		db,
		WithoutEnsureTable(),
		WithListenTimeout(500*time.Millisecond),
	)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error for an unroutable DSN, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got: %v", err)
	}
	// Generous upper bound; the fix's purpose is to avoid hanging forever.
	if elapsed > 5*time.Second {
		t.Fatalf("NewPostgres hung %s before erroring; WithListenTimeout not honored", elapsed)
	}
}

// TestPostgres_TableNameIsolatesChannel: two fanouts with DIFFERENT table
// names on the SAME database must be fully isolated — each derives its
// NOTIFY/LISTEN channel from its table name, so A's large-payload table
// pointers ("t:<id>") are never resolved or re-delivered by B.
func TestPostgres_TableNameIsolatesChannel(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	mk := func(table string) *PostgresFanout {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("open %s: %v", table, err)
		}
		t.Cleanup(func() { db.Close() })
		f, err := NewPostgres(dsn, db, WithTableName(table))
		if err != nil {
			t.Fatalf("NewPostgres %s: %v", table, err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	fA := mk("fanout_a_msgs")
	fB := mk("fanout_b_msgs")

	var gotA, gotB atomic.Int64
	ca, _ := fA.Subscribe("topic", func([]byte) { gotA.Add(1) })
	defer ca()
	cb, _ := fB.Subscribe("topic", func([]byte) { gotB.Add(1) })
	defer cb()

	// Large payload forces the table-fallback path ("t:<id>" pointer).
	big := strings.Repeat("x", 9000)
	if err := fA.Publish(context.Background(), "topic", []byte(big)); err != nil {
		t.Fatalf("Publish A: %v", err)
	}

	// Positive control: A resolves its own pointer.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && gotA.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if gotA.Load() < 1 {
		t.Fatalf("A never received its own table-fallback message (got=%d)", gotA.Load())
	}
	// Negative: B, on a different channel, must not resolve A's pointer.
	time.Sleep(300 * time.Millisecond)
	if got := gotB.Load(); got != 0 {
		t.Fatalf("cross-talk: B delivered %d of A's table-fallback messages (NOTIFY channel must be isolated by table name)", got)
	}
}

// TestPostgres_PublishRejectsInvalidUTF8: invalid UTF-8 in the payload or
// topic is rejected with a descriptive error instead of being silently
// corrupted by Postgres' U+FFFD substitution.
func TestPostgres_PublishRejectsInvalidUTF8(t *testing.T) {
	f, _ := pgFanout(t)

	badPayload := []byte("invalid utf8: \xff\xfe\xfd")
	if err := f.Publish(context.Background(), "topic", badPayload); err == nil {
		t.Fatal("expected error for invalid UTF-8 payload, got nil")
	}
	badTopic := "bad\xbftopic"
	if err := f.Publish(context.Background(), badTopic, []byte("ok")); err == nil {
		t.Fatal("expected error for invalid UTF-8 topic, got nil")
	}
}

// TestPostgres_SlowSubDoesNotStallOtherTopic: a blocking subscriber on topic X
// must not stall delivery to topic Y. A single LISTEN/NOTIFY dispatch goroutine
// serves every topic; before the per-subscriber queue, deliver ran callbacks
// inline, so one blocking callback froze the dispatcher and every other topic.
// Each subscriber now runs on its own bounded queue.
func TestPostgres_SlowSubDoesNotStallOtherTopic(t *testing.T) {
	f, _ := pgFanout(t)

	block := make(chan struct{})
	t.Cleanup(func() { close(block) }) // release the blocking subscriber on exit
	cancelX, _ := f.Subscribe("x", func([]byte) { <-block })
	defer cancelX()

	got := make(chan []byte, 1)
	cancelY, _ := f.Subscribe("y", func(p []byte) {
		select {
		case got <- p:
		default:
		}
	})
	defer cancelY()

	// Publish to X first; under the bug its inline blocking callback wedges the
	// single dispatch goroutine, so Y's NOTIFY is never read.
	if err := f.Publish(context.Background(), "x", []byte("block-me")); err != nil {
		t.Fatalf("Publish x: %v", err)
	}
	time.Sleep(150 * time.Millisecond) // let X's NOTIFY land and (under the bug) wedge dispatch
	if err := f.Publish(context.Background(), "y", []byte("marker")); err != nil {
		t.Fatalf("Publish y: %v", err)
	}
	select {
	case p := <-got:
		if string(p) != "marker" {
			t.Fatalf("got %q, want marker", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("topic Y stalled by topic X's blocking subscriber (dispatch not queue-isolated)")
	}
}

func TestPostgres_CloseStopsSubQueues(t *testing.T) {
	f, _ := pgFanout(t)
	// Subscribe many times, dropping the cancels — Close must stop the
	// per-subscriber queue goroutines anyway or they park forever.
	for i := 0; i < 40; i++ {
		if _, err := f.Subscribe("leak", func([]byte) {}); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
	}
	before := runtime.NumGoroutine()
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= before-35 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscriber-queue goroutines leaked past Close: %d before, %d after",
		before, runtime.NumGoroutine())
}

func TestPostgres_SubscribeAfterCloseErrors(t *testing.T) {
	f, _ := pgFanout(t)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := f.Subscribe("t", func([]byte) {}); err == nil {
		t.Fatal("Subscribe after Close should error, not mint an unstoppable goroutine")
	}
}

func TestPostgres_LongTableNameRejected(t *testing.T) {
	// 74 chars: SafeIdent-legal, but past Postgres's 63-byte NAMEDATALEN —
	// CREATE TABLE would silently truncate while pg_notify errors on every
	// publish. Construction must reject it up front (before touching the DB,
	// so no live Postgres is needed here).
	long := strings.Repeat("a", 74)
	db, err := sql.Open("postgres", "postgres://unused/unused")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := NewPostgres("postgres://unused/unused", db, WithTableName(long)); err == nil {
		t.Fatal("NewPostgres accepted a >63-byte table/channel name")
	}
}

func TestPostgres_SubscribeRejectsBadUTF8Topic(t *testing.T) {
	// Publish already rejects invalid-UTF-8 topics; an accepted subscriber on
	// such a topic could never be matched — reject symmetrically.
	p := &PostgresFanout{subs: map[string]map[uint64]*pgSub{}}
	if _, err := p.Subscribe("top\xffic", func([]byte) {}); err == nil {
		t.Fatal("Subscribe accepted an invalid-UTF-8 topic that Publish rejects")
	}
}
