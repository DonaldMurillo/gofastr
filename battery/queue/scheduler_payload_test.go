package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSchedulerCopiesPayloadOnRegister(t *testing.T) {
	const want = `{"value":"original"}`
	for _, name := range []string{"bytes", "raw-message"} {
		t.Run(name, func(t *testing.T) {
			buf := []byte(want)
			var payload any = buf
			if name == "raw-message" {
				payload = json.RawMessage(buf)
			}
			q := &recordQueue{}
			s := NewScheduler(q)
			t.Cleanup(func() { _ = s.Close() })
			if err := s.Every(time.Minute).Job("email", payload).Register(); err != nil {
				t.Fatalf("Register: %v", err)
			}
			copy(buf, `{"value":"modified"}`)

			s.dispatchDue(context.Background(), time.Now().Add(time.Minute))
			if len(q.jobs) != 1 {
				t.Fatalf("dispatched %d jobs, want 1", len(q.jobs))
			}
			if got := string(q.jobs[0].Payload); got != want {
				t.Fatalf("dispatched payload = %q, want %q", got, want)
			}
		})
	}
}

func TestSchedulerAcceptsEmptyAndNonJSONPayloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload any
		want    json.RawMessage
	}{
		{name: "nil"},
		{name: "nil-bytes", payload: []byte(nil)},
		{name: "nil-raw-message", payload: json.RawMessage(nil)},
		{name: "empty-bytes", payload: []byte{}, want: json.RawMessage{}},
		{name: "empty-raw-message", payload: json.RawMessage{}, want: json.RawMessage{}},
		{name: "empty-string", payload: "", want: json.RawMessage{}},
		{name: "non-json-bytes", payload: []byte("not JSON"), want: json.RawMessage("not JSON")},
		{name: "non-json-raw-message", payload: json.RawMessage("not JSON"), want: json.RawMessage("not JSON")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &recordQueue{}
			s := NewScheduler(q)
			t.Cleanup(func() { _ = s.Close() })
			base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
			if err := s.Every(time.Minute).Job("email", tc.payload).RegisterAt(base); err != nil {
				t.Fatalf("RegisterAt: %v", err)
			}

			s.dispatchDue(context.Background(), base.Add(time.Minute))
			if len(q.jobs) != 1 {
				t.Fatalf("dispatched %d jobs, want 1", len(q.jobs))
			}
			if got := q.jobs[0].Payload; string(got) != string(tc.want) || (got == nil) != (tc.want == nil) {
				t.Fatalf("dispatched payload = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// ROUND-2 REVIEW REPRO.
//
// Job() now rejects a payload that is not valid JSON. The removed
// `if payload == "" { payload = "null" }` fallback in RegisterAt used to
// normalize an EMPTY payload to "null", the same thing Job(jobType, nil)
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
