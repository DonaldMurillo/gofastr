package moduleproto

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// TestRingSinkRetainsLastN: only the last N bytes are retained after writes
// exceed the cap (design §4.2).
func TestRingSinkRetainsLastN(t *testing.T) {
	r := NewRingSink(8)
	r.Write([]byte("abcd"))   // 4 bytes
	r.Write([]byte("efgh"))   // 8 bytes, full
	r.Write([]byte("ijklmn")) // +6 → 14 total; trim to last 8

	got := r.Tail()
	want := []byte("ghijklmn") // last 8 of "abcdefghijklmn"
	if !bytes.Equal(got, want) {
		t.Fatalf("Tail = %q, want %q", got, want)
	}
}

// TestRingSinkNeverBlocks: Write returns immediately regardless of state. We
// approximate "never blocks" by writing far more than the cap from a single
// goroutine and asserting it completes within a deadline.
func TestRingSinkNeverBlocks(t *testing.T) {
	r := NewRingSink(64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1 << 16 {
			// each Write is small but in aggregate far exceeds the cap
			r.Write([]byte("abcdef"))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RingSink.Write blocked")
	}
	if r.Len() > r.Cap() {
		t.Fatalf("ring exceeded cap: %d > %d", r.Len(), r.Cap())
	}
}

// TestRingSinkConcurrentSafe: concurrent writers do not race / corrupt.
// Run with -race to verify.
func TestRingSinkConcurrentSafe(t *testing.T) {
	r := NewRingSink(256)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			buf := make([]byte, 64)
			for i := range buf {
				buf[i] = seed
			}
			for range 200 {
				r.Write(buf)
			}
		}('A' + byte(g))
	}
	wg.Wait()
	if r.Len() > 256 {
		t.Fatalf("Len %d > Cap %d", r.Len(), r.Cap())
	}
}

// TestRingSinkDrainToEOF: Drain copies a reader until EOF, retaining the tail.
func TestRingSinkDrainToEOF(t *testing.T) {
	r := NewRingSink(16)
	// 26 bytes; last 16 retained.
	src := bytes.NewReader([]byte("0123456789ABCDEF0123456789"))
	if err := r.Drain(src); err != nil {
		t.Fatalf("Drain err: %v", err)
	}
	got := r.Tail()
	want := []byte("ABCDEF0123456789") // src[10:26]
	if !bytes.Equal(got, want) {
		t.Fatalf("Tail = %q, want %q", got, want)
	}
}

// TestRingSinkDrainReadError: a non-EOF read error is surfaced (and bytes
// copied before the error are retained).
func TestRingSinkDrainReadError(t *testing.T) {
	r := NewRingSink(64)
	err := r.Drain(errReader{})
	if err == nil {
		t.Fatal("expected Drain to propagate read error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestRingSinkReset clears retained bytes.
func TestRingSinkReset(t *testing.T) {
	r := NewRingSink(16)
	r.Write([]byte("hello"))
	if r.Len() != 5 {
		t.Fatalf("Len = %d, want 5", r.Len())
	}
	r.Reset()
	if r.Len() != 0 {
		t.Fatalf("Len after Reset = %d, want 0", r.Len())
	}
	if len(r.Tail()) != 0 {
		t.Fatalf("Tail not empty after Reset")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
