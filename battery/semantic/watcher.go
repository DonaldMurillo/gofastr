package semantic

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// WatchOptions configures a [Watcher].
type WatchOptions struct {
	// IncludeExts are the file extensions to index, with the dot. Empty means
	// all.
	IncludeExts []string
	// ExcludeDirs are directory base names to skip. Nil means a default set
	// (.git, node_modules and others).
	ExcludeDirs []string
	// PollInterval is the re-scan interval: 0 means 2 seconds, negative scans once.
	PollInterval time.Duration
	// MaxFileSize skips larger files: 0 means 1 MiB, negative no limit.
	MaxFileSize int64
	// MetadataFunc derives chunk metadata from a file's absolute path; nil
	// sets "kind" by extension.
	MetadataFunc func(absPath string) map[string]any
}

// Watcher feeds matching files under roots into an Index and keeps it in sync.
type Watcher struct {
	idx  Index
	opts WatchOptions

	mu         sync.Mutex
	known      map[string]fileFingerprint
	reconciled bool
}

type fileFingerprint struct {
	docID   string
	modTime time.Time
	size    int64
}

// NewWatcher constructs a Watcher over idx.
func NewWatcher(idx Index, opts WatchOptions) *Watcher {
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.MaxFileSize == 0 {
		opts.MaxFileSize = 1 << 20
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

func (w *Watcher) safeMetadata(absPath string) (m map[string]any) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("semantic: MetadataFunc panicked; indexing without metadata",
				"path", absPath, "panic", textsafe.Recovered(rec))
			m = nil
		}
	}()
	return w.opts.MetadataFunc(absPath)
}

func (w *Watcher) Run(ctx context.Context, roots ...string) error {
	if len(roots) == 0 {
		return errors.New("semantic: Watcher.Run requires at least one root")
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

// ScanOnce walks roots once and applies the differences since the last scan.
func (w *Watcher) ScanOnce(ctx context.Context, roots ...string) error {
	return w.scan(ctx, roots)
}

func (w *Watcher) scan(ctx context.Context, roots []string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: watcher scan panicked: %v", v)
		}
	}()
	seen := make(map[string]struct{}, 256)
	absRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("semantic: abs(%q): %w", root, err)
		}
		absRoots = append(absRoots, abs)
		if err := w.scanRoot(ctx, abs, seen); err != nil {
			return err
		}
	}
	w.mu.Lock()
	var gone, deleted []string
	for path, fp := range w.known {
		if _, ok := seen[path]; ok {
			continue
		}
		gone = append(gone, path)
		deleted = append(deleted, fp.docID)
	}
	w.mu.Unlock()
	if len(deleted) > 0 {
		if err := w.idx.Remove(ctx, deleted...); err != nil {
			return fmt.Errorf("semantic: watcher remove: %w", err)
		}
		w.mu.Lock()
		for _, path := range gone {
			delete(w.known, path)
		}
		w.mu.Unlock()
	}
	w.mu.Lock()
	reconciled := w.reconciled
	w.mu.Unlock()
	if !reconciled {
		if err := w.reconcile(ctx, absRoots, seen); err != nil {
			return err
		}
		w.mu.Lock()
		w.reconciled = true
		w.mu.Unlock()
	}
	return nil
}

func (w *Watcher) scanRoot(ctx context.Context, abs string, seen map[string]struct{}) error {
	dir := abs
	if fi, lerr := os.Lstat(abs); lerr == nil && !fi.IsDir() {
		dir = filepath.Dir(abs)
	}
	root, err := openDirRoot(dir)
	if err != nil {
		return fmt.Errorf("semantic: open root %q: %w", abs, err)
	}
	defer root.Close()
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != abs && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if d.IsDir() {
			if slices.Contains(w.opts.ExcludeDirs, name) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !w.matchExt(name) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if w.opts.MaxFileSize > 0 && info.Size() > w.opts.MaxFileSize {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return fmt.Errorf("semantic: rel(%q): %w", path, rerr)
		}
		seen[path] = struct{}{}
		return w.upsert(ctx, root, rel, path, info)
	})
}

func (w *Watcher) readCapped(root *os.Root, rel string) (data []byte, skip bool, err error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		if fi, lerr := root.Lstat(rel); lerr == nil && !fi.Mode().IsRegular() {
			return nil, true, nil
		}
		return nil, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, true, nil
	}
	if w.opts.MaxFileSize < 0 {
		data, err = io.ReadAll(f)
		return data, false, err
	}
	return readLimited(f, w.opts.MaxFileSize)
}

func readLimited(src io.Reader, max int64) (data []byte, skip bool, err error) {
	sentinel := max
	if max < math.MaxInt64 {
		sentinel = max + 1
	}
	data, err = io.ReadAll(io.LimitReader(src, sentinel))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > max {
		return nil, true, nil
	}
	return data, false, nil
}

func (w *Watcher) reconcile(ctx context.Context, absRoots []string, seen map[string]struct{}) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: watcher reconcile panicked: %v", v)
		}
	}()
	dl, ok := w.idx.(docSourceLister)
	if !ok {
		return nil
	}
	sources, err := dl.docSources(ctx)
	if err != nil {
		return fmt.Errorf("semantic: watcher reconcile: %w", err)
	}
	var stale []string
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		src := sources[id]
		if id != pathDocID(src) {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		if !underAnyRoot(src, absRoots) {
			continue
		}
		stale = append(stale, id)
	}
	if len(stale) > 0 {
		if err := w.idx.Remove(ctx, stale...); err != nil {
			return fmt.Errorf("semantic: watcher remove: %w", err)
		}
	}
	return nil
}

func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

func (w *Watcher) upsert(ctx context.Context, root *os.Root, rel, path string, info os.FileInfo) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: watcher upsert %q panicked: %v", path, v)
		}
	}()
	w.mu.Lock()
	prior, ok := w.known[path]
	unchanged := ok && prior.modTime.Equal(info.ModTime()) && prior.size == info.Size()
	w.mu.Unlock()
	if unchanged {
		return nil
	}
	data, skip, err := w.readCapped(root, rel)
	if err != nil {
		return fmt.Errorf("semantic: read %q: %w", path, err)
	}
	if skip {
		if err := w.idx.Remove(ctx, pathDocID(path)); err != nil {
			return fmt.Errorf("semantic: watcher remove %q: %w", path, err)
		}
		w.mu.Lock()
		delete(w.known, path)
		w.mu.Unlock()
		return nil
	}
	docID := pathDocID(path)
	doc := Document{
		ID:       docID,
		Source:   path,
		Text:     string(data),
		Metadata: w.safeMetadata(path),
	}
	if err := w.idx.Add(ctx, doc); err != nil {
		var ce *chunkError
		var invalid *InvalidDocumentTextError
		if !errors.As(err, &ce) && !errors.As(err, &invalid) {
			return fmt.Errorf("semantic: watcher add %q: %w", path, err)
		}
		message := "semantic: watcher skipped a file it cannot chunk"
		if invalid != nil {
			message = "semantic: watcher skipped a file with invalid document text"
		}
		slog.Default().Warn(message,
			"path", textsafe.ScrubControlBytes(path), "err", textsafe.ScrubControlBytes(err.Error()))
		if err := w.idx.Remove(ctx, docID); err != nil {
			return fmt.Errorf("semantic: watcher remove %q: %w", path, err)
		}
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
