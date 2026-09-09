package queue

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// Pins the decode contract the scan paths (db.go, durable_scheduler.go)
// rely on: query.ParseDBTime — the canonical parser queueTime delegated
// to — refuses malformed values instead of returning a zero time.
func TestQueueTimeRejectsMalformedValues(t *testing.T) {
	for _, value := range []any{"not-a-time", []byte("still-not-a-time"), 42, nil} {
		if _, err := query.ParseDBTime(value); err == nil {
			t.Fatalf("query.ParseDBTime(%T) accepted malformed value", value)
		}
	}
}
