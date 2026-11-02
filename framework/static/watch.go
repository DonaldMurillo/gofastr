package static

import (
	"context"
	"fmt"
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
// passed to onError if non-nil. interval 0 keeps the 500ms default; a
// negative interval is rejected — a sign/unit error must not silently
// become a fast poll loop.
//
// Polling avoids a third-party fsnotify dependency. For projects with a
// large source tree this is fine: we only stat files we already know about.
func (b *Builder) Watch(ctx context.Context, watchDirs []string, interval time.Duration, onError func(error)) error {
	if interval < 0 {
		return fmt.Errorf("static: Watch: interval must be >= 0 (got %v)", interval)
	}
	if interval == 0 {
		interval = 500 * time.Millisecond
	}
	if _, err := b.Build(ctx); err != nil {
		if onError != nil {
			onError(err)
		}
	}

	state := newMtimeMap()
	// The export is not a source. OutDir under a watched dir (watch ".",
	// write "./dist") is the common layout, and every rebuild rewrites
	// every export file: polling them made one edit rebuild forever.
	if out, err := filepath.Abs(b.OutDir); err == nil && b.OutDir != "" {
		state.skip = out
	}
	state.scan(watchDirs)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if state.changed(watchDirs) {
				b.log("change detected: rebuilding")
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
	// skip is an absolute directory left out of every walk: the
	// builder's OutDir.
	skip string
}

// skipped reports whether the walk leaves path's subtree out.
func (m *mtimeMap) skipped(path string, d os.DirEntry) bool {
	if m.skip == "" || d == nil || !d.IsDir() {
		return false
	}
	abs, err := filepath.Abs(path)
	return err == nil && abs == m.skip
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
		_ = filepath.WalkDir(d, func(path string, de os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if m.skipped(path, de) {
				return filepath.SkipDir
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
		_ = filepath.WalkDir(d, func(path string, de os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if m.skipped(path, de) {
				return filepath.SkipDir
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
