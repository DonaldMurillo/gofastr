package crud

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// openOwnerStream opens GET /_events as uid and returns a channel of the
// stream's data lines, once the subscription is installed.
func openOwnerStream(t *testing.T, srv *httptest.Server, uid string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?as="+uid, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream as %s: %v", uid, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream as %s: status %d", uid, resp.StatusCode)
	}
	lines := make(chan string, 16)
	subscribed := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		once := false
		for sc.Scan() {
			line := sc.Text()
			if !once && strings.HasPrefix(line, ": subscribed") {
				once = true
				close(subscribed)
			}
			if strings.HasPrefix(line, "data:") {
				lines <- line
			}
		}
		_ = sc.Err() // the stream ends when the test cancels it
	}()
	select {
	case <-subscribed:
	case <-time.After(3 * time.Second):
		t.Fatalf("stream as %s never subscribed", uid)
	}
	return lines
}

// Alice updating Bob's row under owner.AllowCrossOwner publishes an event
// about BOB's record: it reaches Bob's _events stream and never Alice's.
// Stamping the writer's owner sent Bob's row to Alice's subscription.
func TestCrossOwnerEventStampsRecordOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	db.SetMaxOpenConns(1)
	seedRow(t, db, "log-b1", "bob", "bob's row")
	ch.Events = event.NewEventBus()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ch.EventStream()(w, withTestUser(r, r.URL.Query().Get("as")))
	}))
	t.Cleanup(srv.Close)
	alice := openOwnerStream(t, srv, "alice")
	bob := openOwnerStream(t, srv, "bob")

	cross := owner.AllowCrossOwner(withTestUserCtx("alice"))
	if _, err := ch.UpdateOne(cross, "log-b1", map[string]any{"notes": "EDITED-BY-ALICE"}); err != nil {
		t.Fatalf("cross-owner update: %v", err)
	}

	select {
	case line := <-bob:
		if !strings.Contains(line, "EDITED-BY-ALICE") {
			t.Fatalf("bob's stream got %s, want the edited row", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bob's stream never received the update to his own row")
	}
	select {
	case line := <-alice:
		t.Fatalf("alice's stream received bob's row: %s", line)
	case <-time.After(300 * time.Millisecond):
	}
}

// The tenant stamp follows the same rule: a cross-tenant writer in tenant A
// touching tenant B's row publishes to B. The record is read under its wire
// key, so a camelCase handler finds tenantId and userId too.
func TestEventStampsRecordTenantAndOwner(t *testing.T) {
	installOwnerExtractor(t)
	ent := entity.Define("docs", entity.EntityConfig{
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}},
		Scope:  &entity.ScopeConfig{MultiTenant: true, OwnerField: "user_id"},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, nil).WithJSONCase(CaseCamel)

	ctx := tenant.AllowCrossTenant(tenant.SetTenantID(withTestUserCtx("alice"), "A"))
	data := ch.eventData(ctx, map[string]any{"id": "d1", "tenantId": "B", "userId": "bob"})
	if data[eventKeyTenantID] != "B" || data[eventKeyOwnerID] != "bob" {
		t.Fatalf("stamped tenant %v owner %v, want B and bob (the row's)", data[eventKeyTenantID], data[eventKeyOwnerID])
	}
	// A record without the columns falls back to the writer's context.
	data = ch.eventData(ctx, map[string]any{"id": "d1"})
	if data[eventKeyTenantID] != "A" || data[eventKeyOwnerID] != "alice" {
		t.Fatalf("fallback stamped tenant %v owner %v, want A and alice", data[eventKeyTenantID], data[eventKeyOwnerID])
	}
}
