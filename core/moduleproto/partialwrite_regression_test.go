package moduleproto

import (
	"errors"
	"strings"
	"testing"
)

type partialWriter struct{ calls int }

func (p *partialWriter) Write(b []byte) (int, error) {
	p.calls++
	if p.calls == 1 {
		return 1, errors.New("disk full")
	}
	return len(b), nil
}

// After a frame is cut short the stream is out of step, so every later
// frame fails instead of landing in the middle of the broken one.
func TestPartialWriteLatches(t *testing.T) {
	w := &partialWriter{}
	c, err := NewCodec(strings.NewReader(""), w, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.WriteFrame(NewNotification("ping", nil)) == nil {
		t.Fatal("partial write succeeded")
	}
	if c.WriteFrame(NewNotification("ping", nil)) == nil || w.calls != 1 {
		t.Fatalf("frame written after a partial one (writes=%d)", w.calls)
	}
}
