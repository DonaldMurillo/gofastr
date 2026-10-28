package stream

import (
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder is a recorder with the net/http writer's deadline
// surface; it keeps every deadline armed on it.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	armed []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.armed = append(d.armed, t)
	return nil
}

// Every SSEWriter write arms a fresh write deadline, so a stream whose
// client stopped reading cannot block its handler forever: with the
// server's WriteTimeout at 0 (the SSE posture) nothing else bounds the
// write (2026-10-27 bug bash, nowritedeadline).
func TestSSEWriterArmsWriteDeadline(t *testing.T) {
	for name, write := range map[string]func(*SSEWriter) error{
		"event":   func(s *SSEWriter) error { return s.WriteEvent("x", "y") },
		"data":    func(s *SSEWriter) error { return s.WriteData("y") },
		"comment": func(s *SSEWriter) error { return s.WriteComment("ping") },
		"retry":   func(s *SSEWriter) error { s.SetRetry(3); return nil },
		"flush":   func(s *SSEWriter) error { s.Flush(); return nil },
	} {
		t.Run(name, func(t *testing.T) {
			rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			s := NewSSEWriter(rec)
			before := time.Now()
			if err := write(s); err != nil {
				t.Fatal(err)
			}
			if len(rec.armed) == 0 {
				t.Fatal("the write ran with no write deadline")
			}
			if d := rec.armed[len(rec.armed)-1]; !d.After(before) || d.After(before.Add(time.Minute)) {
				t.Fatalf("deadline %v is not a bounded time after the write began", d)
			}
		})
	}
}

// A ResponseWriter chain with no deadline support still streams: the
// deadline is skipped, not the write.
func TestSSEWriterWritesWithoutDeadlineSupport(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := NewSSEWriter(rec).WriteData("y"); err != nil {
		t.Fatalf("write on a recorder with no deadline support: %v", err)
	}
	if rec.Body.String() != "data: y\n\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
