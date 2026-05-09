package static

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watch invokes b.Build whenever a file under any of watchDirs changes its
// modification time, using a polling loop with the given interval. It runs
// one initial build immediately and returns when ctx is cancelled.
//
// The loop ignores errors so a transient compile-time mistake in user code
// (e.g. a half-saved file) does not stop watching; the most recent error is
// passed to onError if non-nil. If interval is <= 0 it defaults to 500ms.
//
// Polling avoids a third-party fsnotify dependency. For projects with a
// large source tree this is fine — we only stat files we already know about.
func (b *Builder) Watch(ctx context.Context, watchDirs []string, interval time.Duration, onError func(error)) error {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if _, err := b.Build(ctx); err != nil {
		if onError != nil {
			onError(err)
		}
	}

	state := newMtimeMap()
	state.scan(watchDirs)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if state.changed(watchDirs) {
				b.log("change detected — rebuilding")
				if _, err := b.Build(ctx); err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}
}

type mtimeMap struct {
	mu    sync.Mutex
	files map[string]time.Time
}

func newMtimeMap() *mtimeMap {
	return &mtimeMap{files: make(map[string]time.Time)}
}

// scan rebuilds the snapshot from disk.
func (m *mtimeMap) scan(dirs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = make(map[string]time.Time)
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				return nil
			}
			m.files[path] = info.ModTime()
			return nil
		})
	}
}

// changed returns true if any tracked file's mtime changed, or if a new file
// appeared under one of the dirs. After detecting a change it refreshes the
// snapshot so the next call reflects the new baseline.
func (m *mtimeMap) changed(dirs []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	dirty := false
	seen := make(map[string]struct{}, len(m.files))

	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				return nil
			}
			seen[path] = struct{}{}
			prev, ok := m.files[path]
			if !ok || !prev.Equal(info.ModTime()) {
				dirty = true
				m.files[path] = info.ModTime()
			}
			return nil
		})
	}

	// File deleted?
	for path := range m.files {
		if _, ok := seen[path]; !ok {
			delete(m.files, path)
			dirty = true
		}
	}
	return dirty
}
