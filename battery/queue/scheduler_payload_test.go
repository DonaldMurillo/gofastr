package queue

import (
	"testing"
	"time"
)

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

func TestSchedulerDispatchOwnsPayload(t *testing.T) {
	first, second := &recordQueue{}, &recordQueue{}
	s := NewScheduler(first, second)
	t.Cleanup(func() { _ = s.Close() })

	const want = `{"v":"A"}`
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	if err := s.Every(time.Minute).Job("email", want).RegisterAt(base); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}
	s.dispatchDue(t.Context(), base.Add(time.Minute))
	if len(first.jobs) != 1 || len(second.jobs) != 1 {
		t.Fatalf("first tick dispatched %d and %d jobs, want one per queue", len(first.jobs), len(second.jobs))
	}
	first.jobs[0].Payload[6] = 'Z'

	t.Run("other-queue", func(t *testing.T) {
		if got := string(second.jobs[0].Payload); got != want {
			t.Fatalf("other queue's payload = %q, want %q", got, want)
		}
	})

	t.Run("next-tick", func(t *testing.T) {
		s.dispatchDue(t.Context(), base.Add(2*time.Minute))
		if len(first.jobs) != 2 {
			t.Fatalf("two ticks dispatched %d jobs to the first queue, want 2", len(first.jobs))
		}
		if got := string(first.jobs[1].Payload); got != want {
			t.Fatalf("next tick's payload = %q, want %q", got, want)
		}
	})
}

func TestSchedulerDispatchPreservesNilAndEmptyPayloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{name: "nil"},
		{name: "empty", payload: []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &recordQueue{}
			s := NewScheduler(q)
			t.Cleanup(func() { _ = s.Close() })

			base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
			if err := s.Every(time.Minute).Job("email", tc.payload).RegisterAt(base); err != nil {
				t.Fatalf("RegisterAt: %v", err)
			}
			s.dispatchDue(t.Context(), base.Add(time.Minute))
			if len(q.jobs) != 1 {
				t.Fatalf("dispatched %d jobs, want 1", len(q.jobs))
			}
			if got := q.jobs[0].Payload; len(got) != 0 || (got == nil) != (tc.payload == nil) {
				t.Fatalf("dispatched payload = %#v, want %#v", got, tc.payload)
			}
		})
	}
}
