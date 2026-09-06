package appstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The state file's integrity, the properties the 2026-09-06 red-probe
// round found missing and the fix (temp file + rename in persist)
// restores: persist never follows a symlink planted at the store's
// path into an overwrite outside the data dir, a concurrent Set and
// Flush never leaves a partial file on disk (a reader at any instant
// sees either the previous whole file or the new whole file), and the
// file is replaced atomically, not rewritten in place. The corrupt
// and unreadable file postures live in appstate_test.go.

func TestSymlinkedStateFileNotFollowed(t *testing.T) {
	outside := t.TempDir()
	store := t.TempDir()
	target := filepath.Join(outside, "victim.json")
	sentinel := `{"sentinel":"do not touch"}`
	if err := os.WriteFile(target, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(store, "state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	s := Open(link, nil)
	if err := s.Set("page.x", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("persist followed the symlink and overwrote the target:\n%s", got)
	}
	// The store must still hold the value for its own readers.
	var v int
	if ok, err := s.Get("page.x", &v); !ok || err != nil || v != 1 {
		t.Fatalf("Get after flush = (%v, %v), want (true, nil) with 1", ok, err)
	}
}

func TestPersistIsAtomicReplace(t *testing.T) {
	setDelay(t, time.Hour)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.zero", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("page.one", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	// An atomic persist swaps the directory entry (write a fresh file,
	// rename over the path). Rewriting the same inode in place means a
	// crash between truncate and write loses the whole store.
	if os.SameFile(before, after) {
		t.Error("persist rewrote the state file in place instead of replacing it atomically")
	}
	// And the file is the whole store either way.
	f := readFile(t, s.Path())
	if _, ok := f.Entries["page.zero"]; !ok || f.Entries["page.one"] == nil {
		t.Fatalf("state file entries = %v, want page.zero and page.one", f.Entries)
	}
}

func TestRacingWritesNeverPartial(t *testing.T) {
	setDelay(t, time.Millisecond)
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := s.Set("page.zero", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	// Corroborating probe for TestPersistIsAtomicReplace: spin
	// readers over the file while writers race Set against Flush, and
	// flag every instant the file is not whole JSON holding page.zero.
	// The window is timing-dependent, so this loop only adds evidence;
	// the inode probe above is the deterministic assertion.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var partials int
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(s.Path())
				if err != nil {
					continue
				}
				bad := len(data) == 0 || !json.Valid(data)
				if !bad {
					var f stateFile
					bad = json.Unmarshal(data, &f) != nil || f.Entries["page.zero"] == nil
				}
				if bad {
					mu.Lock()
					partials++
					mu.Unlock()
				}
			}
		}()
	}

	big := strings.Repeat("b", 32<<10)
	for i := range 200 {
		if err := s.Set("page.k", big+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		if err := s.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if partials > 0 {
		t.Errorf("racing readers saw a partial state file %d times", partials)
	}

	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	f := readFile(t, s.Path())
	if _, ok := f.Entries["page.zero"]; !ok || f.Entries["page.k"] == nil {
		t.Fatalf("final file lost entries: %v", f.Entries)
	}
}
