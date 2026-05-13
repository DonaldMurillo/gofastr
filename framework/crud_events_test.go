package framework

import (
	"bufio"
	"bytes"
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

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func seedEventsDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func eventsApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

// readSSEEvents reads SSE-framed events from rdr until ctx is done. Each
// {event:, data:} pair becomes one entry in the returned channel. Blocking
// reader; cancel ctx to stop.
type sseEvent struct {
	Type string
	Data string
}

func readSSEEvents(t *testing.T, ctx context.Context, body *bufio.Reader) <-chan sseEvent {
	t.Helper()
	out := make(chan sseEvent, 16)
	go func() {
		defer close(out)
		var (
			eventName string
			dataLines []string
		)
		for {
			line, err := body.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				eventName = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
				continue
			}
			if line == "" && eventName != "" {
				select {
				case out <- sseEvent{Type: eventName, Data: strings.Join(dataLines, "\n")}:
				case <-ctx.Done():
					return
				}
				eventName = ""
				dataLines = nil
			}
		}
	}()
	return out
}

// ============================================================================
// Test: SSE stream receives entity.created from a POST
// ============================================================================

func TestSSE_ReceivesCreateEvent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedEventsDB(t, db)
		app := eventsApp(t, db)
		srv := httptest.NewServer(app.Router)
		t.Cleanup(srv.Close)

		streamCtx, cancelStream := context.WithCancel(context.Background())
		defer cancelStream()

		req, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, srv.URL+"/posts/_events", nil)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from SSE endpoint, got %d", resp.StatusCode)
		}
		events := readSSEEvents(t, streamCtx, bufio.NewReader(resp.Body))

		// Trigger a create after the subscription is in place.
		go func() {
			time.Sleep(50 * time.Millisecond)
			body, _ := json.Marshal(map[string]any{"title": "live"})
			_, _ = http.Post(srv.URL+"/posts", "application/json", bytes.NewReader(body))
		}()

		select {
		case ev := <-events:
			if ev.Type != event.EntityCreated {
				t.Fatalf("expected %q, got %q", event.EntityCreated, ev.Type)
			}
			if !strings.Contains(ev.Data, `"entity":"posts"`) {
				t.Fatalf("expected entity=posts in payload, got %s", ev.Data)
			}
			if !strings.Contains(ev.Data, `"title":"live"`) {
				t.Fatalf("expected record.title=live in payload, got %s", ev.Data)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for entity.created event")
		}
	})
}

// ============================================================================
// Test: events for a different entity do not leak into this stream
// ============================================================================

func TestSSE_FiltersByEntity(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		for _, ddl := range []string{
			`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
			`CREATE TABLE comments (id TEXT PRIMARY KEY, body TEXT NOT NULL)`,
		} {
			if _, err := db.Exec(ddl); err != nil {
				t.Fatalf("create: %v", err)
			}
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{Table: "posts", Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}}}.WithTimestamps(false))
		app.Entity("comments", entity.EntityConfig{Table: "comments", Fields: []schema.Field{{Name: "body", Type: schema.String, Required: true}}}.WithTimestamps(false))

		srv := httptest.NewServer(app.Router)
		t.Cleanup(srv.Close)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/posts/_events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		events := readSSEEvents(t, ctx, bufio.NewReader(resp.Body))

		// Drive the wrong entity (comments).
		go func() {
			time.Sleep(50 * time.Millisecond)
			body, _ := json.Marshal(map[string]any{"body": "noise"})
			_, _ = http.Post(srv.URL+"/comments", "application/json", bytes.NewReader(body))
		}()

		select {
		case ev := <-events:
			t.Fatalf("expected no event on /posts stream, got %q payload=%s", ev.Type, ev.Data)
		case <-time.After(300 * time.Millisecond):
			// good — no leak
		}
	})
}

// ============================================================================
// Test: Subscribe/unsubscribe cleans up handlers on client disconnect
// ============================================================================

func TestSSE_DisconnectUnsubscribes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedEventsDB(t, db)
		app := eventsApp(t, db)
		srv := httptest.NewServer(app.Router)
		t.Cleanup(srv.Close)

		bus := app.Events()
		beforeCreate := len(bus.Snapshot(event.EntityCreated))

		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/posts/_events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		// Confirm a handler was added.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if len(bus.Snapshot(event.EntityCreated)) == beforeCreate+1 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if got := len(bus.Snapshot(event.EntityCreated)); got != beforeCreate+1 {
			t.Fatalf("expected one new EntityCreated handler after subscribe, got delta=%d", got-beforeCreate)
		}

		// Disconnect by cancelling the request context and closing the body.
		cancel()
		resp.Body.Close()

		// Wait briefly for the handler goroutine to clean up.
		deadline = time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if len(bus.Snapshot(event.EntityCreated)) == beforeCreate {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("expected handler to be unsubscribed after disconnect, still %d (was %d)",
			len(bus.Snapshot(event.EntityCreated)), beforeCreate)
	})
}

// ============================================================================
// Test: tenant scoping — only events for the connection's tenant pass through
// ============================================================================

func TestSSE_FiltersByTenant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, tenant_id TEXT)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Use(tenant.TenantMiddleware("X-Tenant-ID"))
		app.Entity("posts", entity.EntityConfig{
			Table:       "posts",
			MultiTenant: true,
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))

		srv := httptest.NewServer(app.Router)
		t.Cleanup(srv.Close)

		subscribe := func(tenant string) (<-chan sseEvent, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/posts/_events", nil)
			req.Header.Set("X-Tenant-ID", tenant)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("subscribe %s: %v", tenant, err)
			}
			t.Cleanup(func() { resp.Body.Close() })
			return readSSEEvents(t, ctx, bufio.NewReader(resp.Body)), cancel
		}

		streamA, cancelA := subscribe("tenant-a")
		defer cancelA()
		streamB, cancelB := subscribe("tenant-b")
		defer cancelB()

		// POST as tenant-a.
		go func() {
			time.Sleep(80 * time.Millisecond)
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/posts", strings.NewReader(`{"title":"A only"}`))
			req.Header.Set("X-Tenant-ID", "tenant-a")
			req.Header.Set("Content-Type", "application/json")
			_, _ = http.DefaultClient.Do(req)
		}()

		var (
			gotA, gotB string
			mu         sync.Mutex
			wg         sync.WaitGroup
		)
		wg.Add(2)

		go func() {
			defer wg.Done()
			select {
			case ev := <-streamA:
				mu.Lock()
				gotA = ev.Data
				mu.Unlock()
			case <-time.After(2 * time.Second):
			}
		}()
		go func() {
			defer wg.Done()
			select {
			case ev := <-streamB:
				mu.Lock()
				gotB = ev.Data
				mu.Unlock()
			case <-time.After(700 * time.Millisecond):
			}
		}()
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()
		if gotA == "" {
			t.Fatal("tenant-a stream got nothing")
		}
		if !strings.Contains(gotA, `"title":"A only"`) {
			t.Fatalf("tenant-a payload missing title: %s", gotA)
		}
		if gotB != "" {
			t.Fatalf("tenant-b stream should not have received tenant-a's event, got %s", gotB)
		}
	})
}

// ============================================================================
// Test: missing event bus → 503 (handler with Events=nil)
// ============================================================================

func TestSSE_NoEventBus_503(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	ch := crud.NewCrudHandler(ent, nil)
	ch.Events = nil

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/posts/_events", nil)
	ch.EventStream().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// silence unused import warning when fmt isn't used in fast iteration
var _ = fmt.Sprintf
