//go:build red

package image

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// CONTRACT-QUESTION: the decode-side bomb contract is pixels-only —
// DefaultMaxPixels/Config.MaxPixels cap declared width*height, and no doc
// promises a source-BYTE cap — yet the package docs' own recipe
// (image.go:8 `img, err := image.Decode(r)`) hands Decode untrusted
// readers, and DecodeWithConfig io.ReadAll(r) :74 buffers the entire
// source BEFORE the pixel guard ever runs. Should the package also cap
// source bytes (proposed: 64 MiB default, Config.MaxSourceBytes), the way
// the pixel cap was tightened after round-4?
// Property: untrusted image bytes decode through a source-byte cap —
// buffering stops at a stated bound or the decode errors, instead of
// TotalAlloc tracking delivered bytes without limit.
// Surfaces: framework/image/image.go::DecodeWithConfig :74 —
// `data, err := io.ReadAll(r)` with no bound; the pixel-dims guard in
// decodeBytes only runs after every byte is already resident.
// Finding (probe shape): a peer that dribbles PNG-magic garbage forever
// makes the CLI/server goroutine buffer its entire send stream in heap
// before any validation refuses it; the pixel cap never gets the chance.
// Fix direction: read the source through io.LimitReader(r, MaxSourceBytes)
// (64 MiB default, Config.MaxSourceBytes for larger legit inputs) and
// error — or decode streaming — so the guard is reachable before the
// buffer is.

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDecodeRedCapsSourceBytes streams a never-ending PNG-magic source
// past the proposed 64 MiB cap and asserts that either Decode has already
// returned an error or TotalAlloc stopped below the cap. Today ReadAll
// keeps buffering whatever the reader delivers: allocation tracks the
// peer's send rate with no ceiling.
func TestDecodeRedCapsSourceBytes(t *testing.T) {
	const capBytes = 64 << 20  // proposed default cap (Config.MaxSourceBytes)
	const threshold = 72 << 20 // cap + 8 MiB slack past which buffering must stop

	r := newSrcCapRedReader()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	done := make(chan error, 1)
	go func() {
		_, err := Decode(r)
		done <- err
	}()

	var decodeErr error
	returned := false
	checkDone := func() bool {
		if returned {
			return true
		}
		select {
		case decodeErr = <-done:
			returned = true
		default:
		}
		return returned
	}

	deadline := time.Now().Add(10 * time.Second)
	for r.delivered.Load() < threshold && !checkDone() {
		if time.Now().After(deadline) {
			r.cancel()
			t.Fatal("setup broken: reader never delivered past the cap threshold")
		}
		time.Sleep(time.Millisecond)
	}

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	delta := m1.TotalAlloc - m0.TotalAlloc

	if delta > capBytes && !(returned && decodeErr != nil) {
		t.Errorf("SECURITY: [image-decode-unbounded-readall] Decode buffered %d source bytes (still running: %v, err so far: %v) with no ceiling — DecodeWithConfig :74 io.ReadAll(r) runs BEFORE the pixel-dims guard, so a dribbling peer pins heap at its own send rate; proposed cap: 64 MiB default via Config.MaxSourceBytes, error past the cap", delta, !returned, decodeErr)
	}

	// Bounded cleanup: cancel the source and join the goroutine.
	r.cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("setup broken: Decode did not return after source cancel")
	}
}

// srcCapRedReader yields PNG-magic-prefixed zero chunks forever, paced
// lightly, counting every byte delivered. Cancel makes the next Read
// return an error so the consumer goroutine can end.
type srcCapRedReader struct {
	stopCh    chan struct{}
	stopOne   sync.Once
	delivered atomic.Int64
	chunk     []byte
}

func newSrcCapRedReader() *srcCapRedReader {
	chunk := make([]byte, 64<<10)
	copy(chunk, []byte{137, 80, 78, 71, 13, 10, 26, 10}) // PNG signature
	return &srcCapRedReader{stopCh: make(chan struct{}), chunk: chunk}
}

func (r *srcCapRedReader) Read(p []byte) (int, error) {
	select {
	case <-r.stopCh:
		return 0, errors.New("srcCapRedReader: canceled")
	default:
	}
	n := copy(p, r.chunk)
	r.delivered.Add(int64(n))
	time.Sleep(50 * time.Microsecond) // pace: deliver slowly, never finish
	runtime.Gosched()
	return n, nil
}

func (r *srcCapRedReader) cancel() {
	r.stopOne.Do(func() { close(r.stopCh) })
}
