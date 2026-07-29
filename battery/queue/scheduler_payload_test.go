package queue

import (
	"testing"
	"time"
)

// ROUND-2 REVIEW REPRO.
//
// Job() now rejects a payload that is not valid JSON. The removed
// `if payload == "" { payload = "null" }` fallback in RegisterAt used to
// normalize an EMPTY payload to "null" — the same thing Job(jobType, nil)
// still produces. An empty []byte / json.RawMessage / "" is "no payload",
// not a malformed one, and it now fails registration instead.
//
// Reached by any caller that marshals conditionally:
//
//	var body []byte
//	if opts != nil { body, _ = json.Marshal(opts) }
//	sched.Every(id, d).Job("email", body).Register()
//
// Run:
//
//	go test ./battery/queue/ -run TestEmptyPayloadMeansNull -v
func TestEmptyPayloadMeansNull(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload any
	}{
		{name: "nil-bytes", payload: []byte(nil)},
		{name: "empty-bytes", payload: []byte{}},
		{name: "empty-string", payload: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openDurableSchedulerDB(t)
			q := newDurableTestQueue(t, db)
			scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{OwnerID: "empty-payload"})
			if err != nil {
				t.Fatalf("NewDurableScheduler: %v", err)
			}
			err = scheduler.Every("empty-payload", time.Minute).
				Job("email", tc.payload).
				RegisterAt(time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("RegisterAt rejected an EMPTY payload (%q): %v — "+
					"before this round it was normalized to \"null\", the same as Job(t, nil)",
					tc.name, err)
			}
			var payload string
			if err := db.QueryRow("SELECT payload FROM "+q.schedulerSchedulesTable()+" WHERE id=$1", "empty-payload").
				Scan(&payload); err != nil {
				t.Fatalf("read payload: %v", err)
			}
			if payload != "null" {
				t.Fatalf("payload = %q, want null", payload)
			}
		})
	}
}
