// Package appstate is the desktop battery's durable key/value store:
// one JSON file, 0600, debounced writes, a flush on quit, and a change
// hook. Values are stored as raw JSON, so a value written through one
// type reads back through another without ever being re-encoded
// through any. Keys are namespaced by their leading dotted segments;
// the battery owns "windows" and "settings", the page owns "page.".
//
// EXPERIMENTAL: part of battery/desktop.
package appstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// MaxValueBytes caps one value, measured on its encoded JSON.
const MaxValueBytes = 64 << 10

// ErrKey is returned for a key off the grammar.
var ErrKey = errors.New("appstate: key must match ^[a-z][a-z0-9_.-]{0,63}$")

// ErrTooLarge is returned for a value whose encoded JSON exceeds
// MaxValueBytes.
var ErrTooLarge = fmt.Errorf("appstate: value exceeds %d bytes of encoded JSON", MaxValueBytes)

// reKey is the key grammar: a lowercase letter, then up to 63 lowercase
// letters, digits, dots, underscores, or dashes. Dots are namespaces
// (windows, settings, page.theme).
var reKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// writeDelay is the debounce window: every write reschedules, so one
// write lands this long after the last change. The tests override it
// (same package) to keep the suite fast.
var writeDelay = 500 * time.Millisecond

// stateFile is the on-disk shape. The encoder sorts the keys; the file
// is always written whole.
type stateFile struct {
	Entries map[string]json.RawMessage `json:"entries"`
}

// Store is a durable key/value store backed by one JSON file. Open
// reads the file (a missing file is an empty store; the parent
// directory must exist; the file is created on the first write, 0600).
// Safe for concurrent use.
type Store struct {
	logger *slog.Logger
	path   string

	// mu guards entries, dirty, timer, and watchers. The timer's write
	// and Flush take it too, so a flush always sees the latest state.
	mu        sync.Mutex
	entries   map[string]json.RawMessage
	dirty     bool
	timer     *time.Timer
	watchers  map[int]func(key string)
	nextWatch int

	// writeMu serializes the whole write: the snapshot for a persist
	// and the file write itself both happen under it, so a debounced
	// write racing the quit flush can neither interleave partial files
	// nor land a stale snapshot after a newer flush.
	writeMu sync.Mutex
}

// Open reads path into a Store. A missing file is an empty store; a
// corrupt or unreadable one is a Warn and an empty store (a truncated
// quit-time write must not brick the next launch); the next write
// replaces it. A nil logger means slog.Default().
func Open(path string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Store{
		logger:  logger,
		path:    path,
		entries: make(map[string]json.RawMessage),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("appstate: reading the state file failed; starting empty",
				"file", filepath.Base(path), "error", err.Error())
		}
		return s
	}
	var f stateFile
	if err := json.Unmarshal(data, &f); err != nil || f.Entries == nil {
		logger.Warn("appstate: the state file is unreadable; starting empty; the next write replaces it",
			"file", filepath.Base(path))
		return s
	}
	s.entries = f.Entries
	return s
}

// Get decodes the value stored under key into out. It answers (false,
// nil) when the key is absent and an error when the key is off the
// grammar or the stored value does not decode into out.
func (s *Store) Get(key string, out any) (bool, error) {
	if !reKey.MatchString(key) {
		return false, fmt.Errorf("%w: %q", ErrKey, key)
	}
	s.mu.Lock()
	raw, ok := s.entries[key]
	var data []byte
	if ok {
		// Copy under the lock: the caller owns the bytes it decodes.
		data = make([]byte, len(raw))
		copy(data, raw)
	}
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("appstate: decode %q: %w", key, err)
	}
	return true, nil
}

// Get is the typed convenience over Store.Get.
func Get[T any](s *Store, key string) (T, bool, error) {
	var out T
	ok, err := s.Get(key, &out)
	return out, ok, err
}

// Set stores v under key as JSON and schedules the debounced write.
// It answers ErrKey for a key off the grammar and ErrTooLarge when
// the encoded value exceeds MaxValueBytes.
func (s *Store) Set(key string, v any) error {
	if !reKey.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrKey, key)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("appstate: encode %q: %w", key, err)
	}
	if len(data) > MaxValueBytes {
		return fmt.Errorf("%w: %d bytes", ErrTooLarge, len(data))
	}
	s.mu.Lock()
	s.entries[key] = json.RawMessage(data)
	s.scheduleLocked()
	s.mu.Unlock()
	s.notify(key)
	return nil
}

// Delete removes key. Deleting an absent key succeeds, marks nothing
// dirty, and still notifies (every Set and Delete announces itself).
func (s *Store) Delete(key string) error {
	if !reKey.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrKey, key)
	}
	s.mu.Lock()
	if _, ok := s.entries[key]; ok {
		delete(s.entries, key)
		s.scheduleLocked()
	}
	s.mu.Unlock()
	s.notify(key)
	return nil
}

// Keys returns the stored keys that start with prefix, sorted.
func (s *Store) Keys(prefix string) []string {
	s.mu.Lock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	s.mu.Unlock()
	slices.Sort(keys)
	return keys
}

// Watch calls fn with the key of every Set and Delete, on fn's own
// goroutine, never under the store's lock. The returned stop removes
// the watcher; calling it twice is a no-op.
func (s *Store) Watch(fn func(key string)) (stop func()) {
	s.mu.Lock()
	if s.watchers == nil {
		s.watchers = make(map[int]func(key string))
	}
	id := s.nextWatch
	s.nextWatch++
	s.watchers[id] = fn
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.watchers, id)
			s.mu.Unlock()
		})
	}
}

// Flush persists any pending change now and waits for any in-flight
// write to finish (the quit path): when it returns, no persist is
// pending and the file on disk holds every change that completed
// before the call. A store with nothing dirty and nothing in flight
// is a no-op.
func (s *Store) Flush() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	entries := s.snapshotLocked()
	s.mu.Unlock()
	return s.persist(entries)
}

// Path is the file the store persists to.
func (s *Store) Path() string { return s.path }

// scheduleLocked arms the debounced write; the caller holds mu.
func (s *Store) scheduleLocked() {
	s.dirty = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(writeDelay, s.write)
}

// write is the debounce timer's callback: a flush that swallows its
// own error (the next write or the quit flush reports it). Sharing
// Flush's path is the point: one debounce/flush ordering, so a fired
// timer's write and a quit flush serialize on writeMu, the snapshot
// is taken under the same lock as the persist, and neither can land a
// stale file after the other.
func (s *Store) write() { _ = s.Flush() }

// snapshotLocked copies the entries for a write and clears dirty; the
// caller holds mu (and, on every path that persists, writeMu).
func (s *Store) snapshotLocked() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(s.entries))
	maps.Copy(out, s.entries)
	s.dirty = false
	return out
}

// persist writes the entries whole through a fresh temp file in the
// same directory renamed onto the store's path: the rename is the
// atomic swap (a reader at any instant sees the previous whole file
// or the new one, never a torn one) and it replaces the path's
// directory entry, so a symlink planted at the path is swapped out,
// never followed. The temp file is 0600 and the rename keeps that
// mode. The caller holds writeMu. A failed write is a Warn and an
// error, never a panic: the state stays in memory and the next write
// retries.
func (s *Store) persist(entries map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(stateFile{Entries: entries}, "", "  ")
	if err != nil {
		s.logger.Warn("appstate: encoding the state file failed", "error", err.Error())
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		s.logger.Warn("appstate: creating the temp state file failed",
			"file", filepath.Base(s.path), "error", err.Error())
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		s.logger.Warn("appstate: writing the state file failed",
			"file", filepath.Base(s.path), "error", err.Error())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		s.logger.Warn("appstate: closing the state file failed",
			"file", filepath.Base(s.path), "error", err.Error())
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		s.logger.Warn("appstate: replacing the state file failed",
			"file", filepath.Base(s.path), "error", err.Error())
		return err
	}
	return nil
}

// notify hands one change to every watcher on its own goroutine. The
// callbacks are snapshotted under the lock and invoked after it, so a
// watcher can call back into the store.
func (s *Store) notify(key string) {
	s.mu.Lock()
	fns := make([]func(string), 0, len(s.watchers))
	for _, fn := range s.watchers {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		go fn(key)
	}
}
