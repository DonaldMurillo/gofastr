package embed

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WatchOptions configures a [Watcher].
type WatchOptions struct {
	// IncludeExts is the set of file extensions to index, including the
	// leading dot (".go", ".md"). Empty means all files.
	IncludeExts []string
	// ExcludeDirs is a set of directory names to skip entirely, matched
	// on the path's base name. Defaults to a sensible set if nil:
	// {".git", "node_modules", "dist", ".gofastr", "vendor"}.
	ExcludeDirs []string
	// PollInterval is how often the watcher re-scans roots after the
	// initial walk. Defaults to 2 seconds. Set to <= 0 to disable
	// polling (initial walk only).
	PollInterval time.Duration
	// MaxFileSize skips files larger than this. 0 means 1 MiB. Negative
	// means no limit.
	MaxFileSize int64
	// MetadataFunc derives chunk metadata from the absolute path of a
	// file being indexed. When nil, a default sets {"kind": "code"} for
	// .go files and {"kind": "doc"} for .md/.txt; everything else is
	// untagged.
	MetadataFunc func(absPath string) map[string]any
}

// Watcher walks one or more filesystem roots, feeds matching files
// into an [Index] as [Document]s, and (optionally) re-scans on a
// timer to keep the index in sync with changes.
//
// The watcher uses polling rather than OS-level file events so it has
// no third-party dependency. Cost on small trees (~thousands of
// files) is negligible; on very large trees, swap for an
// fsnotify-backed implementation by replacing this file.
type Watcher struct {
	idx  Index
	opts WatchOptions

	mu    sync.Mutex
	known map[string]fileFingerprint // absolute path -> last seen state
}

type fileFingerprint struct {
	docID   string
	modTime time.Time
	size    int64
}

// NewWatcher constructs a Watcher. The Index is fed via [Index.Add]
// and [Index.Remove] as files appear, change, and disappear.
func NewWatcher(idx Index, opts WatchOptions) *Watcher {
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.MaxFileSize == 0 {
		opts.MaxFileSize = 1 << 20 // 1 MiB
	}
	if len(opts.ExcludeDirs) == 0 {
		opts.ExcludeDirs = []string{".git", "node_modules", "dist", ".gofastr", "vendor"}
	}
	if opts.MetadataFunc == nil {
		opts.MetadataFunc = defaultMetadata
	}
	return &Watcher{
		idx:   idx,
		opts:  opts,
		known: make(map[string]fileFingerprint),
	}
}

// Run walks the provided roots once, indexes every matching file, and
// (unless PollInterval <= 0) loops re-scanning on every tick until
// ctx is canceled. Returns ctx.Err() on cancellation, or the first
// indexing error encountered.
func (w *Watcher) Run(ctx context.Context, roots ...string) error {
	if len(roots) == 0 {
		return errors.New("embed: Watcher.Run requires at least one root")
	}
	if err := w.scan(ctx, roots); err != nil {
		return err
	}
	if w.opts.PollInterval <= 0 {
		return nil
	}
	t := time.NewTicker(w.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := w.scan(ctx, roots); err != nil {
				return err
			}
		}
	}
}

// ScanOnce walks roots once and applies any diffs against the last
// known state. Useful for tests and for the `gofastr embed index`
// one-shot CLI mode.
func (w *Watcher) ScanOnce(ctx context.Context, roots ...string) error {
	return w.scan(ctx, roots)
}

func (w *Watcher) scan(ctx context.Context, roots []string) error {
	seen := make(map[string]struct{}, 256)
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("embed: abs(%q): %w", root, err)
		}
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			name := d.Name()
			if d.IsDir() {
				for _, ex := range w.opts.ExcludeDirs {
					if name == ex {
						return fs.SkipDir
					}
				}
				return nil
			}
			if !w.matchExt(name) {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				return nil // racing deletion; skip
			}
			if w.opts.MaxFileSize > 0 && info.Size() > w.opts.MaxFileSize {
				return nil
			}
			seen[path] = struct{}{}
			return w.upsert(ctx, path, info)
		})
		if err != nil {
			return err
		}
	}
	// Anything previously known that wasn't seen this scan was deleted.
	w.mu.Lock()
	var deleted []string
	for path, fp := range w.known {
		if _, ok := seen[path]; ok {
			continue
		}
		deleted = append(deleted, fp.docID)
		delete(w.known, path)
	}
	w.mu.Unlock()
	if len(deleted) > 0 {
		if err := w.idx.Remove(ctx, deleted...); err != nil {
			return fmt.Errorf("embed: watcher remove: %w", err)
		}
	}
	return nil
}

func (w *Watcher) upsert(ctx context.Context, path string, info os.FileInfo) error {
	w.mu.Lock()
	prior, ok := w.known[path]
	unchanged := ok && prior.modTime.Equal(info.ModTime()) && prior.size == info.Size()
	w.mu.Unlock()
	if unchanged {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Race with a deletion is fine; treat as a skip.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("embed: read %q: %w", path, err)
	}
	docID := pathDocID(path)
	doc := Document{
		ID:       docID,
		Source:   path,
		Text:     string(data),
		Metadata: w.opts.MetadataFunc(path),
	}
	if err := w.idx.Add(ctx, doc); err != nil {
		return fmt.Errorf("embed: watcher add %q: %w", path, err)
	}
	w.mu.Lock()
	w.known[path] = fileFingerprint{docID: docID, modTime: info.ModTime(), size: info.Size()}
	w.mu.Unlock()
	return nil
}

func (w *Watcher) matchExt(name string) bool {
	if len(w.opts.IncludeExts) == 0 {
		return true
	}
	for _, ext := range w.opts.IncludeExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// pathDocID hashes the path so doc IDs are stable across runs but
// short enough to look at. The full path is kept in Document.Source.
func pathDocID(path string) string {
	h := sha1.Sum([]byte(path))
	return "file-" + hex.EncodeToString(h[:8])
}

func defaultMetadata(path string) map[string]any {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return map[string]any{"kind": "code", "lang": "go"}
	case ".md", ".markdown":
		return map[string]any{"kind": "doc", "lang": "markdown"}
	case ".txt":
		return map[string]any{"kind": "doc"}
	default:
		return nil
	}
}
