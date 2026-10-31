package fanout

import (
	"testing"
	"time"
)

// A spilled payload expires on the purge clock whether or not anyone
// publishes another large message (S29): the dispatcher purges on its
// own ticker, so an erased user's oversized event does not wait for
// the next big publish.
func TestPurgeRunsWithoutPublish(t *testing.T) {
	_, db := pgFanout(t, func(p *PostgresFanout) { p.purgeEvery = 50 * time.Millisecond })
	if _, err := db.Exec(`INSERT INTO gofastr_fanout_msgs (payload, created_at) VALUES ($1, $2)`,
		"stale", time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM gofastr_fanout_msgs`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stale spill row still present after %s with no publish", 3*time.Second)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
