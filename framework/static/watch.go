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
	state := &mtimeMap{}
	if b.OutDir != "" {
		state.skip = resolvedWatchPath(b.OutDir)
	}
	if dir, name, err := b.pageManifest(); err == nil && b.OutDir != "" {
		state.skipFile = filepath.Join(resolvedWatchPath(dir), name)
	}
	state.files = state.scan(watchDirs)
	if _, err := b.Build(ctx); err != nil && onError != nil {
		onError(err)
	}

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

func resolvedWatchPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	if parent := filepath.Dir(abs); parent != abs {
		return filepath.Join(resolvedWatchPath(parent), filepath.Base(abs))
	}
	return abs
}

func (m *mtimeMap) scan(dirs []string) map[string]time.Time {
	files := make(map[string]time.Time)
	for _, d := range dirs {
		_ = filepath.WalkDir(resolvedWatchPath(d), func(path string, de os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == m.skip && de.IsDir() {
				return filepath.SkipDir
			}
			if path == m.skipFile {
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
