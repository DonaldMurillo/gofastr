package webhook

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A paused subscriber read back from a store and re-subscribed with one
// field changed must stay paused, on every store.
func TestPausedSubscriberSurvivesReadModifySubscribe(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "p.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sqlStore, err := NewSQLStore(db, WithSQLAllowPlaintext())
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]Store{"memory": NewMemoryStore(), "sql": sqlStore} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			m := New(store, Options{AllowPrivateNetworks: true})
			if _, err := m.Subscribe(ctx, Subscriber{ID: "s1", URL: "https://example.com/h", Secret: "sek", Paused: true}); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetSubscriber(ctx, "s1")
			if err != nil || got == nil {
				t.Fatalf("got=%v err=%v", got, err)
			}
			if !got.Paused || got.Active {
				t.Fatalf("read back Active=%v Paused=%v; want a paused subscriber", got.Active, got.Paused)
			}
			got.Secret = "rotated"
			if _, err := m.Subscribe(ctx, *got); err != nil {
				t.Fatal(err)
			}
			if n, _ := m.Publish(ctx, "order.created", []byte(`{}`)); n != 0 {
				t.Fatalf("queued %d deliveries to a paused subscriber", n)
			}
		})
	}
}

// The inbound settles ride out a transient SQLITE_BUSY the way the
// outbound settles do (TestClaimSettlesRetrySQLiteBusy).
func TestInboundSettlesRetrySQLiteBusy(t *testing.T) {
	for _, arm := range []string{"processing", "update", "dedupe"} {
		t.Run(arm, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "busy.db"))+"?_busy_timeout=1&_journal_mode=WAL")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(3)
			store, err := NewSQLInboundStore(db)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			env := InboundEnvelope{ID: "e1", Source: "src", Payload: []byte("{}"), Status: InboundStatusReceived, ReceivedAt: now, UpdatedAt: now}
			if err := store.AddEnvelope(ctx, env); err != nil {
				t.Fatal(err)
			}
			blocker, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Close()
			if _, err := blocker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			unlocked := make(chan error, 1)
			time.AfterFunc(75*time.Millisecond, func() {
				_, err := blocker.ExecContext(ctx, "ROLLBACK")
				unlocked <- err
			})
			switch arm {
			case "processing":
				var ok bool
				ok, err = store.MarkEnvelopeProcessing(ctx, "e1")
				if err == nil && !ok {
					err = errors.New("no row matched")
				}
			case "update":
				env.Status = InboundStatusProcessed
				err = store.UpdateEnvelope(ctx, env)
			case "dedupe":
				err = store.RegisterDedupeKey(ctx, "e1", "k")
			}
			if unlockErr := <-unlocked; unlockErr != nil {
				t.Fatal(unlockErr)
			}
			if err != nil {
				t.Fatalf("inbound write failed under transient contention: %v", err)
			}
		})
	}
}

// A credential the receiver echoes across ResponseBodyLimit must not
// leave a prefix of itself in the stored body.
func TestDeliveryRedactsCredentialCutByBodyLimit(t *testing.T) {
	const token = "tok-SUPERSECRETVALUE-0123456789abcdef" // not-a-secret: test fixture credential
	for pad := 20; pad <= 64; pad++ {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(strings.Repeat("x", pad) + r.Header.Get("Authorization") + " tail"))
		}))
		d := deliverOnce(t, Subscriber{URL: srv.URL, Auth: Auth{Kind: AuthBearer, Token: token}}, Options{ResponseBodyLimit: 64})
		srv.Close()
		rest := strings.ReplaceAll(d.ResponseBody, redactedCredential, "")
		if i := strings.Index(rest, "tok-"); i >= 0 {
			t.Fatalf("pad %d: stored body holds a prefix of the credential: %q", pad, d.ResponseBody)
		}
		if len(d.ResponseBody) > 64 {
			t.Fatalf("pad %d: stored body is %d bytes; the limit is 64", pad, len(d.ResponseBody))
		}
	}
}

// A second echo that the read cut short must not slide into the kept
// window when the first one shrinks to the placeholder.
func TestDeliveryRedactsRepeatedCredentialEcho(t *testing.T) {
	token := "tok-" + strings.Repeat("S3CR3T", 11) // not-a-secret: test fixture credential
	for gap := 0; gap <= 70; gap++ {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(token + strings.Repeat("x", gap) + token + "tail"))
		}))
		d := deliverOnce(t, Subscriber{URL: srv.URL, Auth: Auth{Kind: AuthBearer, Token: token}}, Options{ResponseBodyLimit: 64})
		srv.Close()
		if strings.Contains(d.ResponseBody, "S3CR3T") {
			t.Fatalf("gap %d: stored body holds part of the credential: %q", gap, d.ResponseBody)
		}
		if len(d.ResponseBody) > 64 {
			t.Fatalf("gap %d: stored body is %d bytes; the limit is 64", gap, len(d.ResponseBody))
		}
	}
}

type failingClaimStore struct{ *MemoryStore }

func (failingClaimStore) DueDeliveries(context.Context, time.Time, int) ([]Delivery, error) {
	return nil, errors.New("db down")
}

func (failingClaimStore) ClaimDueDeliveries(context.Context, time.Time, int, time.Duration) ([]Delivery, error) {
	return nil, errors.New("db down")
}

// A worker that cannot read its queue says so.
func TestTickLogsDueDeliveriesError(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	m := New(failingClaimStore{NewMemoryStore()}, Options{Logger: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}})
	m.tick(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(logs) == 0 || !strings.Contains(strings.Join(logs, "\n"), "db down") {
		t.Fatalf("logs = %q; want the store error", logs)
	}
}
