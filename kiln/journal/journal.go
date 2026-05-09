package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Journal is an append-only event log. Implementations must be safe for
// concurrent Append callers but are not required to support concurrent
// Read with Append (callers serialize replay against writes).
type Journal interface {
	// Append writes one entry. Returns the entry's stored offset (1-based)
	// for callers that want to reference it (e.g. for TruncateAfter).
	Append(Entry) (offset int, err error)

	// Read returns every entry in order.
	Read() ([]Entry, error)

	// TruncateAfter drops entries whose 1-based offset is greater than n.
	// TruncateAfter(0) clears the log. Used to implement undo.
	TruncateAfter(n int) error

	// Len reports the number of entries currently stored.
	Len() (int, error)

	// Close releases any underlying resources. Safe to call on an
	// in-memory journal.
	Close() error
}

// --- Memory implementation ---------------------------------------------

// Memory is an in-memory Journal suitable for tests and ephemeral sessions.
// The zero value is usable. Methods are safe for concurrent use.
type Memory struct {
	mu      sync.RWMutex
	entries []Entry
}

// NewMemory returns a fresh in-memory Journal.
func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) Append(e Entry) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return len(m.entries), nil
}

func (m *Memory) Read() ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	return out, nil
}

func (m *Memory) TruncateAfter(n int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 0 {
		return fmt.Errorf("journal: truncate offset must be >= 0, got %d", n)
	}
	if n > len(m.entries) {
		return fmt.Errorf("journal: truncate offset %d exceeds length %d", n, len(m.entries))
	}
	m.entries = m.entries[:n]
	return nil
}

func (m *Memory) Len() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries), nil
}

func (*Memory) Close() error { return nil }

// --- JSONL implementation ----------------------------------------------

// JSONL is a journal backed by a JSON-lines file on disk. Each Append
// writes one line and fsyncs so a crash loses at most the in-flight entry.
// TruncateAfter rewrites the file atomically via rename.
type JSONL struct {
	path string

	mu      sync.Mutex
	file    *os.File
	count   int // entries in file (read at Open, maintained on Append/Truncate)
}

// OpenJSONL opens or creates a JSONL journal at path. The parent directory
// must exist. The file is opened append-only; counts are initialized by
// scanning the existing file.
func OpenJSONL(path string) (*JSONL, error) {
	if path == "" {
		return nil, errors.New("journal: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("journal: ensure dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("journal: open %s: %w", path, err)
	}
	count, err := countLines(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("journal: count entries in %s: %w", path, err)
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("journal: seek end: %w", err)
	}
	return &JSONL{path: path, file: f, count: count}, nil
}

func countLines(f *os.File) (int, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	n := 0
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		n++
	}
	return n, scanner.Err()
}

func (j *JSONL) Append(e Entry) (int, error) {
	buf, err := json.Marshal(e)
	if err != nil {
		return 0, fmt.Errorf("journal: marshal entry: %w", err)
	}
	buf = append(buf, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.file.Write(buf); err != nil {
		return 0, fmt.Errorf("journal: write: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		return 0, fmt.Errorf("journal: fsync: %w", err)
	}
	j.count++
	return j.count, nil
}

func (j *JSONL) Read() ([]Entry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("journal: seek start: %w", err)
	}
	defer j.file.Seek(0, io.SeekEnd) //nolint:errcheck
	scanner := bufio.NewScanner(j.file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var entries []Entry
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("journal: parse entry %d in %s: %w", len(entries)+1, j.path, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("journal: scan: %w", err)
	}
	return entries, nil
}

func (j *JSONL) TruncateAfter(n int) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n < 0 {
		return fmt.Errorf("journal: truncate offset must be >= 0, got %d", n)
	}
	if n > j.count {
		return fmt.Errorf("journal: truncate offset %d exceeds length %d", n, j.count)
	}
	if n == j.count {
		return nil
	}
	// Rewrite atomically: copy first n lines to tmp, rename over original.
	if _, err := j.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("journal: seek start: %w", err)
	}
	tmpPath := j.path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("journal: open tmp: %w", err)
	}
	scanner := bufio.NewScanner(j.file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	written := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if written >= n {
			break
		}
		if _, err := tmp.Write(line); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("journal: write tmp: %w", err)
		}
		if _, err := tmp.Write([]byte{'\n'}); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("journal: write tmp: %w", err)
		}
		written++
	}
	if err := scanner.Err(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("journal: scan: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("journal: tmp fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("journal: tmp close: %w", err)
	}
	// Replace original.
	if err := j.file.Close(); err != nil {
		return fmt.Errorf("journal: close original: %w", err)
	}
	if err := os.Rename(tmpPath, j.path); err != nil {
		return fmt.Errorf("journal: rename: %w", err)
	}
	f, err := os.OpenFile(j.path, os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("journal: reopen: %w", err)
	}
	j.file = f
	j.count = n
	return nil
}

func (j *JSONL) Len() (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.count, nil
}

func (j *JSONL) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}
