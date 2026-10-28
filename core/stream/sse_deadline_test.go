package stream

import (
	"io"
	"net/http"
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
			if len(rec.armed) < 2 {
				t.Fatalf("armed %v: want a bounded deadline, then a clear", rec.armed)
			}
			if d := rec.armed[0]; !d.After(before) || d.After(before.Add(time.Minute)) {
				t.Fatalf("deadline %v is not a bounded time after the write began", d)
			}
			if d := rec.armed[len(rec.armed)-1]; !d.IsZero() {
				t.Fatalf("deadline %v is still armed after the write", d)
			}
		})
	}
}

// On HTTP/2 a write deadline is a stream timer: it resets the stream when
// it fires, write pending or not. An SSE stream idles between events far
// longer than one write's bound (the broker's heartbeat is 30s), so each
// write clears its deadline once flushed (2026-10-28 review of the
// nowritedeadline fix).
func TestSSEWriterSurvivesIdleOnHTTP2(t *testing.T) {
	old := sseWriteTimeout
	sseWriteTimeout = 100 * time.Millisecond
	t.Cleanup(func() { sseWriteTimeout = old })

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewSSEWriter(w)
		for range 2 {
			if err := s.WriteData("x"); err != nil {
				return
			}
			time.Sleep(4 * sseWriteTimeout)
		}
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("proto = %s, want HTTP/2", resp.Proto)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream reset while idle: %v (read %q)", err, body)
	}
	if string(body) != "data: x\n\ndata: x\n\n" {
		t.Fatalf("body = %q", body)
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
