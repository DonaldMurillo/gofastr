package crud

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestBatchRace_ConcurrentCreatePreservesOwner verifies that concurrent
// batch-create operations preserve the owner from the auth context,
// not from the request body. Run with -race flag.
// Attack: race condition allows one user's batch to stamp another user's
// owner ID.
func TestBatchRace_ConcurrentCreatePreservesOwner(t *testing.T) {

	const iterations = 100
	for iter := 0; iter < iterations; iter++ {
		ch, db := setupSecurityTestHandler(t, makeEntityConfig("race_notes", "race_notes", "user_id", []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "content", Type: schema.String},
		}), `CREATE TABLE race_notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, content TEXT)`)

		concurrency := runtime.GOMAXPROCS(0) * 10
		if concurrency > 20 {
			concurrency = 20 // cap for test runtime
		}

		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(userIdx int) {
				defer wg.Done()
				userID := "alice"
				if userIdx%2 == 1 {
					userID = "bob"
				}

				req := makeRequest(t, RequestOpts{
					Method: http.MethodPost,
					Path:   "/race_notes",
					Body:   `{"content":"test data"}`,
					UserID: userID,
				})
				rr := httptest.NewRecorder()
				ch.Create()(rr, req)
			}(i)
		}
		wg.Wait()

		// Verify no cross-contamination: every row's user_id should match
		// the auth context that created it, not some other goroutine's user.
		rows, err := db.Query("SELECT user_id, content FROM race_notes")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var uid, content string
			if err := rows.Scan(&uid, &content); err != nil {
				t.Fatal(err)
			}
			if uid != "alice" && uid != "bob" {
				t.Errorf("SECURITY: [batch_race] unexpected user_id=%q in row (content=%q). Attack: race condition in concurrent create mixed up owner context.", uid, content)
			}
		}
	}
}

// TestBatchRace_ConcurrentDeleteScopedToOwner verifies that concurrent
// batch-delete operations only delete the requesting user's records.
// Run with -race flag.
// Attack: race condition in batch delete removes another user's records.
func TestBatchRace_ConcurrentDeleteScopedToOwner(t *testing.T) {

	const iterations = 50
	for iter := 0; iter < iterations; iter++ {
		ch, db := setupSecurityTestHandler(t, makeEntityConfig("race_del", "race_del", "user_id", []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "content", Type: schema.String},
		}), `CREATE TABLE race_del (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, content TEXT)`)

		// Seed bob's record
		seedRows(t, db, "race_del", []map[string]any{
			{"id": "bob-keep", "user_id": "bob", "content": "bob must survive"},
			{"id": "alice-del", "user_id": "alice", "content": "alice to delete"},
		})

		// Alice concurrently tries to delete her record
		// while bob also operates concurrently
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			req := makeRequest(t, RequestOpts{
				Method: http.MethodDelete,
				Path:   "/race_del/alice-del",
				UserID: "alice",
			})
			req.SetPathValue("id", "alice-del")
			rr := httptest.NewRecorder()
			ch.Delete()(rr, req)
		}()

		go func() {
			defer wg.Done()
			req := makeRequest(t, RequestOpts{
				Method: http.MethodGet,
				Path:   "/race_del/bob-keep",
				UserID: "bob",
			})
			req.SetPathValue("id", "bob-keep")
			rr := httptest.NewRecorder()
			ch.Get()(rr, req)
		}()

		wg.Wait()

		// Bob's record must survive
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM race_del WHERE id = ?", "bob-keep").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("SECURITY: [batch_race] bob's record was deleted by concurrent operations. Attack: race condition in concurrent delete cross-contaminated owner scope.")
		}
	}
}

// suppress unused import
var _ = schema.String
