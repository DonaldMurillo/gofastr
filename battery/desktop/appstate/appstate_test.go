package appstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The store unit level: round trip, the guards, the debounce, the
// flush, corruption, the file mode, watch, and the write race. The
// page-facing behavior on top of the store (the state capability,
// persistence across a whole Run) lives in battery/desktop's harness
// suites.

// setDelay points the debounce window at d for one test and restores
// the default after.
func setDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := writeDelay
	writeDelay = d
	t.Cleanup(func() { writeDelay = old })
}

// logBuffer is a mutex-guarded Warn sink.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&lockedWriter{b: b}, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type lockedWriter struct{ b *logBuffer }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	return w.b.buf.Write(p)
}

// waitFor polls cond every 5ms for up to 5s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cond() {
		return
	}
	t.Fatalf("timed out waiting for %s", what)
}

// readFile decodes the store file.
func readFile(t *testing.T, path string) stateFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var f stateFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%s does not decode: %v\n%s", path, err, data)
	}
	return f
}

func TestSetGetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := Open(path, nil)

	type view struct {
		Zoom float64 `json:"zoom"`
	}
	if err := s.Set("settings.view", view{Zoom: 1.25}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got, ok, err := Get[view](s, "settings.view")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Zoom != 1.25 {
		t.Fatalf("Get returned %+v, want zoom 1.25", got)
	}

	// A fresh store over the same file reads the value back: the file,
	// not the memory, is the contract.
	s2 := Open(path, nil)
	if s2.Path() != path {
		t.Fatalf("Path() = %q, want %q", s2.Path(), path)
	}
	got2, ok, err := Get[view](s2, "settings.view")
	if err != nil || !ok {
		t.Fatalf("reopen Get: ok=%v err=%v", ok, err)
	}
	if got2.Zoom != 1.25 {
		t.Fatalf("reopen returned %+v, want zoom 1.25", got2)
	}
}

func TestAbsentKey(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	var v map[string]int
	ok, err := s.Get("page.nothing", &v)
	if ok || err != nil {
		t.Fatalf("Get absent: ok=%v err=%v, want (false, nil)", ok, err)
	}
	if v != nil {
		t.Fatalf("Get touched out on a miss: %+v", v)
	}
}

func TestBadKeyRefused(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	for _, key := range []string{
		"", "Bad", "1x", "with space", "page.Upper", "page/", strings.Repeat("k", 65),
	} {
		if err := s.Set(key, 1); !errors.Is(err, ErrKey) {
			t.Fatalf("Set(%q) err = %v, want ErrKey", key, err)
		}
		if _, err := s.Get(key, new(int)); !errors.Is(err, ErrKey) {
			t.Fatalf("Get(%q) err = %v, want ErrKey", key, err)
		}
		if err := s.Delete(key); !errors.Is(err, ErrKey) {
			t.Fatalf("Delete(%q) err = %v, want ErrKey", key, err)
		}
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("a refused key still created the file")
	}
}

func TestOversizeValueRefused(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	// The quotes JSON adds push a MaxValueBytes string over the cap.
	if err := s.Set("page.big", strings.Repeat("a", MaxValueBytes)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Set err = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("an oversize value still created the file")
	}
	// The cap itself (the encoded JSON, quotes included) passes.
	if err := s.Set("page.edge", strings.Repeat("a", MaxValueBytes-2)); err != nil {
		t.Fatalf("Set at the cap: %v", err)
	}
}

func TestDebounceWritesAfterDelay(t *testing.T) {
	setDelay(t, 200*time.Millisecond)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.a", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("the write landed before the debounce delay elapsed")
	}
	waitFor(t, "the debounced write", func() bool {
		_, err := os.Stat(s.Path())
		return err == nil
	})
	if f := readFile(t, s.Path()); string(f.Entries["page.a"]) != "1" {
		t.Fatalf("debounced file holds %s, want page.a = 1", f.Entries["page.a"])
	}
}

func TestFlushLandsImmediately(t *testing.T) {
	setDelay(t, 10*time.Second)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.now", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// No waiting: the file is there the moment Flush returns, and the
	// 10s debounce never had a chance.
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatalf("Flush left no file: %v", err)
	}
	if f := readFile(t, s.Path()); string(f.Entries["page.now"]) != "true" {
		t.Fatalf("flushed file holds %s, want page.now = true", f.Entries["page.now"])
	}
}

func TestCorruptFileIsWarnAndEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	// The shape a truncated quit-time write leaves behind.
	if err := os.WriteFile(path, []byte(`{"entries": {"main": {"fr`), 0o600); err != nil {
		t.Fatal(err)
	}
	lg := new(logBuffer)
	s := Open(path, lg.logger())
	var n int
	if ok, err := s.Get("page.anything", &n); ok || err != nil {
		t.Fatalf("Get over a corrupt file: ok=%v err=%v, want (false, nil)", ok, err)
	}
	if msg := lg.String(); !strings.Contains(msg, "unreadable") {
		t.Fatalf("no Warn about the corrupt file; logs were:\n%s", msg)
	}
	// The next write replaces the corrupt bytes with a whole file.
	if err := s.Set("page.fixed", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if f := readFile(t, s.Path()); string(f.Entries["page.fixed"]) != "1" {
		t.Fatalf("file after replace holds %s", f.Entries["page.fixed"])
	}
}

func TestUnreadableFileIsWarnAndEmpty(t *testing.T) {
	// The path is a directory: ReadFile fails for a reason other than
	// absence, which is still a Warn and an empty store, never a panic.
	lg := new(logBuffer)
	s := Open(t.TempDir(), lg.logger())
	if ok, err := s.Get("page.a", new(int)); ok || err != nil {
		t.Fatalf("Get over an unreadable file: ok=%v err=%v", ok, err)
	}
	if msg := lg.String(); !strings.Contains(msg, "reading the state file failed") {
		t.Fatalf("no Warn about the read failure; logs were:\n%s", msg)
	}
}

func TestFileModeIs0600(t *testing.T) {
	setDelay(t, time.Hour)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.a", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestWatchFiresAndStops(t *testing.T) {
	setDelay(t, time.Hour)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	ch := make(chan string, 16)
	stop := s.Watch(func(key string) { ch <- key })

	if err := s.Set("page.a", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("page.a"); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		select {
		case k := <-ch:
			if k != "page.a" {
				t.Fatalf("watch %d fired with %q, want page.a", i, k)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("watch did not fire for change %d", i)
		}
	}

	stop()
	if err := s.Set("page.b", 2); err != nil {
		t.Fatal(err)
	}
	select {
	case k := <-ch:
		t.Fatalf("a stopped watch still fired: %q", k)
	case <-time.After(150 * time.Millisecond):
	}
	stop() // idempotent
}

func TestDeleteAbsentWritesNothing(t *testing.T) {
	setDelay(t, 50*time.Millisecond)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Delete("page.gone"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("deleting an absent key wrote the file")
	}
}

func TestKeysSortedWithPrefix(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	for _, k := range []string{"page.b", "page.a", "windows", "page.c.d"} {
		if err := s.Set(k, 1); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.Keys("page."); strings.Join(got, ",") != "page.a,page.b,page.c.d" {
		t.Fatalf("Keys(page.) = %v", got)
	}
	if got := s.Keys(""); strings.Join(got, ",") != "page.a,page.b,page.c.d,windows" {
		t.Fatalf("Keys() = %v", got)
	}
	if got := s.Keys("nothing."); len(got) != 0 {
		t.Fatalf("Keys(nothing.) = %v, want empty", got)
	}
}

func TestGetTypeMismatchErrors(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.n", 1); err != nil {
		t.Fatal(err)
	}
	var str string
	if ok, err := s.Get("page.n", &str); ok || err == nil {
		t.Fatalf("Get into the wrong type: ok=%v err=%v, want a decode error", ok, err)
	}
}

func TestFlushRacingTimerWholeFile(t *testing.T) {
	setDelay(t, 20*time.Millisecond)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	// page.zero predates every racer, so every snapshot any writer can
	// hold contains it; deletes never run.
	if err := s.Set("page.zero", 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = s.Flush()
			}
		}()
	}
	for i := range 50 {
		if err := s.Set(fmt.Sprintf("page.k%d", i), i); err != nil {
			t.Fatal(err)
		}
		if err := s.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	// Every racer has returned, so no persist is in flight; one final
	// flush after that, and the file must be a whole readable file.
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	f := readFile(t, s.Path())
	if _, ok := f.Entries["page.zero"]; !ok {
		t.Fatal("the raced file lost page.zero")
	}
	if _, ok := f.Entries["page.k0"]; !ok {
		t.Fatal("the raced file lost page.k0")
	}
}
