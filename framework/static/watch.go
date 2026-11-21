package static

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watch runs Build once, then again whenever a file under watchDirs
// changes, polling every interval (0 means 500ms), until ctx is done.
// Build errors go to onError, when set, and the watch goes on. A negative
// interval is an error.
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

	state := &mtimeMap{}
	if out, err := filepath.Abs(b.OutDir); err == nil && b.OutDir != "" {
		state.skip = out
	}
	if dir, name, err := b.pageManifest(); err == nil && b.OutDir != "" {
		state.skipFile = filepath.Join(dir, name)
	}
	state.files = state.scan(watchDirs)

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
	mu       sync.Mutex
	files    map[string]time.Time
	skip     string
	skipFile string
}

func (m *mtimeMap) skipped(path string, d os.DirEntry) bool {
	if m.skip == "" || d == nil || !d.IsDir() {
		return false
	}
	abs, err := filepath.Abs(path)
	return err == nil && abs == m.skip
}

func (m *mtimeMap) ignored(path string) bool {
	if m.skipFile == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	return err == nil && abs == m.skipFile
}

func (m *mtimeMap) scan(dirs []string) map[string]time.Time {
	files := make(map[string]time.Time)
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(path string, de os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if m.skipped(path, de) {
				return filepath.SkipDir
			}
			if m.ignored(path) {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				return nil
			}
			files[path] = info.ModTime()
			return nil
		})
	}
	return files
}

func (m *mtimeMap) changed(dirs []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	files := m.scan(dirs)
	dirty := !maps.EqualFunc(m.files, files, time.Time.Equal)
	m.files = files
	return dirty
}
