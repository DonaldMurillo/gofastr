package outbox

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

var outboxDialects = []struct {
	name string
	open func(*testing.T, ...Option) (*sql.DB, *Outbox)
}{
	{"sqlite", openOutbox},
	{"postgres", pgOutbox},
}

func TestLongOutboxTableParity(t *testing.T) {
	for _, dialect := range outboxDialects {
		t.Run(dialect.name, func(t *testing.T) {
			prefix := strings.Repeat("a", 62)
			db, first := dialect.open(t, WithTable(prefix+"x"))
			second, err := New(db, WithTable(prefix+"y"))
			if err != nil {
				t.Fatal(err)
			}
			stmt := "SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name"
			if dialect.name == "postgres" {
				stmt = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() ORDER BY table_name"
			}
			rows, err := db.Query(stmt)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var names []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				names = append(names, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want := []string{prefix + "x", prefix + "y", query.FitIdent(prefix + "x_delivery"), query.FitIdent(prefix + "y_delivery")}
			slices.Sort(want)
			if !slices.Equal(names, want) {
				t.Errorf("tables = %v, want %v", names, want)
			}
			for _, o := range []*Outbox{first, second} {
				id := appendCommitted(t, db, o, "event")
				if _, err := o.expandDeliveries(t.Context()); err != nil {
					t.Errorf("expand: %v", err)
					continue
				}
				ds := mustDeliveries(t, o, id)
				if len(ds) != 1 || ds[0].Consumer != "c" {
					t.Errorf("delivery: %+v, want one delivery for c", ds)
				}
			}
			if _, err := New(db, WithTable(prefix+"zz"), WithoutEnsureTable()); err == nil {
				t.Error("New accepted a 64-byte table name")
			}
		})
	}
}

func TestLargeDeliveryClaimParity(t *testing.T) {
	const batch = 1200
	for _, dialect := range outboxDialects {
		t.Run(dialect.name, func(t *testing.T) {
			db, o := dialect.open(t, WithBatchSize(batch))
			id := appendCommitted(t, db, o, "event")
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for i := range batch + 1 {
				if _, err := tx.ExecContext(t.Context(), `INSERT INTO event_outbox_delivery
					(row_id, consumer, created_at) VALUES ($1, $2, $3)`,
					id, fmt.Sprintf("consumer-%d", i), o.now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}

			for _, want := range []int{batch, 1, 0} {
				got, err := o.claimDeliveries(t.Context())
				if err != nil || len(got) != want {
					t.Fatalf("claim: got %d, %v; want %d", len(got), err, want)
				}
				for _, d := range got {
					if d.Attempts != 1 {
						t.Errorf("claim %q: attempts = %d, want 1", d.Consumer, d.Attempts)
					}
				}
			}
			var leased int
			if err := db.QueryRow(`SELECT count(*) FROM event_outbox_delivery
				WHERE attempts = 1 AND claimed_until IS NOT NULL`).Scan(&leased); err != nil || leased != batch+1 {
				t.Fatalf("persisted leases: got %d, %v; want %d", leased, err, batch+1)
			}
		})
	}
}

func TestWideDeliveryAttemptBudgetParity(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("requires a 64-bit int")
	}
	wideBudget := int64(1) << 31
	for _, dialect := range outboxDialects {
		t.Run(dialect.name, func(t *testing.T) {
			db, o := dialect.open(t, WithMaxAttempts(int(wideBudget)))
			now := time.Now().UTC()
			o.now = func() time.Time { return now }
			id := appendCommitted(t, db, o, "event")
			if _, err := o.expandDeliveries(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, want := range []int{1, 2} {
				got, err := o.claimDeliveries(t.Context())
				if err != nil || len(got) != 1 {
					t.Fatalf("claim %d: got %d, %v; want one delivery", want, len(got), err)
				}
				if d := got[0]; d.RowID != id || d.Attempts != want {
					t.Errorf("claim %d: got row %q with %d attempts, want %q with %d attempts",
						want, d.RowID, d.Attempts, id, want)
				}
				now = now.Add(o.lease + time.Second)
			}
		})
	}
}
