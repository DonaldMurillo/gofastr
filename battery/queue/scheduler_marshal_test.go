package queue

import (
	"math"
	"testing"
	"time"
)

// A payload json.Marshal refuses fails registration, as the durable
// builder's does. Job dropped the error, so a NaN or a channel registered
// fine and every run received a null payload.
func TestSchedulePayloadMarshalError(t *testing.T) {
	for name, payload := range map[string]any{
		"nan":     map[string]float64{"ratio": math.NaN()},
		"channel": make(chan int),
	} {
		t.Run(name, func(t *testing.T) {
			s := NewScheduler()
			if err := s.Every(time.Minute).Job("report", payload).Register(); err == nil {
				t.Fatal("a payload that cannot be encoded registered")
			}
			if n := len(s.schedules); n != 0 {
				t.Fatalf("%d schedules kept after the refusal, want 0", n)
			}
		})
	}
	// Job called again with a good payload clears the earlier error.
	s := NewScheduler()
	b := s.Every(time.Minute).Job("report", math.NaN()).Job("report", map[string]int{"n": 1})
	if err := b.Register(); err != nil {
		t.Fatalf("a builder whose last payload encodes was refused: %v", err)
	}
}

// A second Job call with nil replaces the first call's payload: the builder
// encodes each call, it does not merge them.
func TestSecondJobCallWithNilPayloadClearsTheFirst(t *testing.T) {
	s := NewScheduler()
	b := s.Every(time.Minute).Job("report", map[string]int{"n": 1}).Job("cleanup", nil)
	if err := b.RegisterAt(time.Now()); err != nil {
		t.Fatalf("RegisterAt: %v", err)
	}
	if len(s.schedules) != 1 {
		t.Fatalf("%d schedules, want 1", len(s.schedules))
	}
	if p := s.schedules[0].Payload; len(p) != 0 {
		t.Fatalf("payload = %q, want no payload", string(p))
	}
}
