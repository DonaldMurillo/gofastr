// Package appstate is the desktop battery's durable key and value store:
// one 0600 JSON file with debounced writes, a quit flush and change watchers.
//
// EXPERIMENTAL: part of battery/desktop.
package appstate

import (
	"bytes"
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

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// MaxValueBytes caps one value, measured on compact JSON without HTML escaping.
const MaxValueBytes = 64 << 10

// ErrKey is returned for a key off the grammar.
var ErrKey = errors.New("appstate: key must match ^[a-z][a-z0-9_.-]{0,63}$")

// ErrTooLarge is returned for a value whose encoded JSON exceeds MaxValueBytes.
var ErrTooLarge = fmt.Errorf("appstate: value exceeds %d bytes of encoded JSON", MaxValueBytes)

var reKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

var writeDelay = 500 * time.Millisecond

type stateFile struct {
	Entries map[string]json.RawMessage `json:"entries"`
}

// Store is a durable key/value store backed by one JSON file.
type Store struct {
	logger *slog.Logger
	path   string

	mu        sync.Mutex
	entries   map[string]json.RawMessage
	dirty     bool
	timer     *time.Timer
	watchers  map[int]func(key string)
	nextWatch int

	writeMu sync.Mutex
}

// Open reads path into a Store.
func Open(path string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Store{
		logger:  logger,
		path:    path,
		entries: make(map[string]json.RawMessage),
	}
	data, err := atomicfile.ReadFile(path)
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

// Get decodes the value stored under key into out.
func (s *Store) Get(key string, out any) (bool, error) {
	if !reKey.MatchString(key) {
		return false, fmt.Errorf("%w: %q", ErrKey, key)
	}
	s.mu.Lock()
	raw, ok := s.entries[key]
	var data []byte
	if ok {
		data = make([]byte, len(raw))
		copy(data, raw)
	}
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := jsonexact.Unmarshal(data, out); err != nil {
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
func (s *Store) Set(key string, v any) error {
	if !reKey.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrKey, key)
	}
	data, err := encodeJSON(v, "")
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

// Delete removes key.
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

// Watch calls fn with the key of every Set and Delete, on fn's own goroutine, never under the store's lock.
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

// Flush persists any pending change and waits for an in-flight write to finish.
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
	if err := s.persist(entries); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

// Path is the file the store persists to.
func (s *Store) Path() string { return s.path }

func (s *Store) scheduleLocked() {
	s.dirty = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(writeDelay, s.write)
}

func (s *Store) write() { _ = s.Flush() }

func (s *Store) snapshotLocked() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(s.entries))
	maps.Copy(out, s.entries)
	s.dirty = false
	return out
}

func (s *Store) persist(entries map[string]json.RawMessage) error {
	data, err := encodeJSON(stateFile{Entries: entries}, "  ")
	if err != nil {
		s.logger.Warn("appstate: encoding the state file failed", "error", err.Error())
		return err
	}
	stage, err := atomicfile.WriteFileNoFollow(s.path, data, fileperm.RestrictOpenFile)
	if err == nil || stage == atomicfile.WritePrepareTemp {
		return err
	}
	var action string
	switch stage {
	case atomicfile.WriteCreateTemp:
		action = "creating the temp"
	case atomicfile.WriteData:
		action = "writing the"
	case atomicfile.WriteCloseTemp:
		action = "closing the"
	case atomicfile.WriteReplace:
		action = "replacing the"
	default:
		action = "persisting the"
	}
	s.logger.Warn("appstate: "+action+" state file failed",
		"file", filepath.Base(s.path), "error", err.Error())
	return err
}

func encodeJSON(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (s *Store) notify(key string) {
	s.mu.Lock()
	fns := make([]func(string), 0, len(s.watchers))
	for _, fn := range s.watchers {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		deliver(s.logger, fn, key)
	}
}

func deliver(logger *slog.Logger, fn func(string), key string) {
	go func() {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("appstate: watcher panicked", "key", key, "panic", textsafe.Recovered(v))
			}
		}()
		fn(key)
	}()
}
