package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// F18 layer-mismatch parsing — pinned 2026-09-05 round 4.
// Family: F18 layer-mismatch parsing
// Property: a JSON number the API accepts into an Int column must be persisted exactly as
// sent or refused with 400 — never silently rounded to a different value.
// Surfaces: crud.go:Create, crud.go:Update, crud_batch.go:BatchCreate (items),
//   crud_api.go:CreateOne/UpdateOne (map bodies decoded by host json.Unmarshal),
//   crud_upsert.go:UpsertOne (caller-supplied AutoIncrement pk via incrementBindValue),
//   mcp.go:createTool/updateTool (params re-marshalled to JSON then decoded).
//   Read-side surfaces are EXEMPT: ?where= refuses numeric values outright
//   (string-typed), and ?field=<int> filter strings bind exact.
// Fix (this suite's green state): write bodies decode with json.Decoder.UseNumber and
//   normalize integer literals to exact int64 (numberexact.go); a float64 that reaches
//   an Int column at |f| >= 2^53 is refused with 400 — crud cannot tell a legitimately
//   round number from one an upstream float64 decode rounded. The same value sent as a
//   STRING round-trips exactly.

// bigSent is 2^53+1: the first integer JSON/float64 cannot represent.
const bigSent = int64(9007199254740993)

func intLedger(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	return setupSecurityTestHandler(t, makeEntityConfig("wire_ledger", "wire_ledger", "", []schema.Field{
		{Name: "amount", Type: schema.Int},
	}), `CREATE TABLE wire_ledger (id TEXT PRIMARY KEY, amount INTEGER)`)
}

func storedAmount(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT amount FROM wire_ledger LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("read back stored amount: %v", err)
	}
	return got
}

// TestIntJSONRoundTripsOrRefuses drives every write surface with the same
// JSON integer and requires exact persistence (or an explicit refusal).
func TestIntJSONRoundTripsOrRefuses(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T) (stored int64, refused bool)
	}{
		{
			name: "http create",
			run: func(t *testing.T) (int64, bool) {
				ch, db := intLedger(t)
				req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/wire_ledger",
					Body: `{"amount": 9007199254740993}`, UserID: "alice"})
				rr := httptest.NewRecorder()
				ch.Create()(rr, req)
				if rr.Code == http.StatusBadRequest {
					return 0, true
				}
				if rr.Code != http.StatusCreated {
					t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
				}
				return storedAmount(t, db), false
			},
		},
		{
			name: "http update",
			run: func(t *testing.T) (int64, bool) {
				ch, db := intLedger(t)
				seedRows(t, db, "wire_ledger", []map[string]any{{"id": "r1", "amount": 1}})
				req := makeRequest(t, RequestOpts{Method: http.MethodPatch, Path: "/wire_ledger/r1",
					Body: `{"amount": 9007199254740993}`, UserID: "alice"})
				req.SetPathValue("id", "r1")
				rr := httptest.NewRecorder()
				ch.Update()(rr, req)
				if rr.Code == http.StatusBadRequest {
					return 0, true
				}
				if rr.Code != http.StatusOK {
					t.Fatalf("update status=%d body=%s", rr.Code, rr.Body.String())
				}
				return storedAmount(t, db), false
			},
		},
		{
			name: "batch create item",
			run: func(t *testing.T) (int64, bool) {
				ch, db := intLedger(t)
				req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/wire_ledger/_batch",
					Body: `{"items":[{"amount":9007199254740993}]}`, UserID: "alice"})
				rr := httptest.NewRecorder()
				ch.BatchCreate()(rr, req)
				if rr.Code == http.StatusBadRequest {
					return 0, true
				}
				if rr.Code != http.StatusOK {
					t.Fatalf("batch status=%d body=%s", rr.Code, rr.Body.String())
				}
				return storedAmount(t, db), false
			},
		},
		{
			// A host that json.Unmarshals its own body loses the literal to
			// float64 before crud sees it; the only correct answer left is
			// refusal (coerceIntColumnValues, the |f| >= 2^53 gate).
			name: "in-process create with json-decoded body",
			run: func(t *testing.T) (int64, bool) {
				ch, db := intLedger(t)
				var body map[string]any
				if err := json.Unmarshal([]byte(`{"amount": 9007199254740993}`), &body); err != nil {
					t.Fatal(err)
				}
				uctx := handler.SetUser(context.Background(), &testUser{id: "alice"})
				if _, err := ch.CreateOne(uctx, body); err != nil {
					return 0, true
				}
				return storedAmount(t, db), false
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, refused := tc.run(t)
			if refused {
				return // refusal is the acceptable alternative
			}
			if got != bigSent {
				t.Errorf("SECURITY: [int-precision]: %s accepted 9007199254740993 but persisted %d (silent precision loss); must store it exactly or refuse with 400", tc.name, got)
			}
		})
	}

	// Control: the same value as a STRING round-trips exactly, proving the
	// column and driver can hold it and the loss is specific to the
	// JSON-number decode path.
	t.Run("string spelling control", func(t *testing.T) {
		ch, db := intLedger(t)
		req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/wire_ledger",
			Body: `{"amount": "9007199254740993"}`, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.Create()(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("control create status=%d body=%s", rr.Code, rr.Body.String())
		}
		if got := storedAmount(t, db); got != bigSent {
			t.Fatalf("control: string form stored %d, want exact — if the string form also corrupts, the finding moves to the bind layer", got)
		}
	})
}
