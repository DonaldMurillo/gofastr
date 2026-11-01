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
	// reconciled is set once a scan has compared the index against the
	// tree. known starts empty, so until then the deletion pass cannot
	// see a file deleted while no watcher ran.
	reconciled bool
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
// indexing error encountered. A file the chunker refuses (invalid UTF-8,
// a failing fallback) is not an error: it is logged, left out of the
// index, and retried once it changes.
// safeMetadata runs the app-supplied metadata callback under a recover
// guard: it fires on the watcher loop, which has no per-request net, so
// watcher.
func (w *Watcher) safeMetadata(absPath string) (m map[string]any) {
	defer func() {
		if rec := recover(); rec != nil {
			// The recovered value can embed metadata-derived text with
			// raw control/bidi bytes (terminal injection on the watcher
			// loop, where no HTTP recover net exists), so it goes to the
			// sink scrubbed like every pinned recover path.
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

// ScanOnce walks roots once and applies any diffs against the last
// known state. Useful for tests and for the `gofastr semantic index`
// one-shot CLI mode.
func (w *Watcher) ScanOnce(ctx context.Context, roots ...string) error {
	return w.scan(ctx, roots)
}

func (w *Watcher) scan(ctx context.Context, roots []string) (err error) {
	// w.idx is a host-supplied Index reached from Run's polling loop,
	// which has no per-request recover net. A panic in the index (or
	// anywhere in the walk) degrades to the same attributed error a
	// failing Add/Remove already produces — Run reports it and stops,
	// the process survives.
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
	// Anything previously known that wasn't seen this scan was deleted.
	// Its fingerprint is dropped only once Remove succeeds: forgotten
	// first, a failed Remove was never retried, since the reconcile that
	// would catch it runs once.
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
		// Dropped even when a concurrent scan re-added the path: Remove
		// took that document too (its ID is the path's), and the next
		// scan adds it back.
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

// scanRoot walks one absolute root and upserts every matching regular
// file. Files are read through an os.Root opened on the root, so a path
// swapped for a link that leads out of it between the walk and the read
// is refused by the kernel, never followed. Symlinks, devices, sockets
// and pipes are skipped at the walk: a link's own Lstat says nothing
// about its target, which could be ~/.ssh/id_ed25519 or /dev/zero.
//
// A root that names a file (ScanOnce(ctx, "README.md")) is confined to
// its parent directory, since os.OpenRoot opens only a directory; the
// walk still visits that one file. The directory is opened as one
// (openDirRoot), so a root swapped for a FIFO after the Lstat fails the
// open instead of blocking it.
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
			return nil // racing deletion; skip
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

// readCapped reads rel through root: only a regular file, and at most
// MaxFileSize bytes even when the file grew since the walk measured it.
// skip reports a file that is no longer one to index (gone, replaced by
// something that is not a regular file, or grown past the limit).
//
// The open does not wait (openNonblock): a file swapped for a FIFO after
// the walk would otherwise block the open until a writer appeared, and
// no context reaches an open(2). The regular-file check on the handle
// then skips it; a regular file reads the same with the flag set.
func (w *Watcher) readCapped(root *os.Root, rel string) (data []byte, skip bool, err error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil // racing deletion
		}
		if fi, lerr := root.Lstat(rel); lerr == nil && !fi.Mode().IsRegular() {
			return nil, true, nil // swapped for a link out of the root
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

// readLimited reads at most max+1 bytes of src: max or fewer is the
// file, one more says it grew past the limit (skip), and the rest of a
// file that keeps growing is never read.
func readLimited(src io.Reader, max int64) (data []byte, skip bool, err error) {
	data, err = io.ReadAll(io.LimitReader(src, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > max {
		return nil, true, nil
	}
	return data, false, nil
}

// reconcile removes the documents the index holds for files under roots
// that this scan did not see: files deleted while no watcher ran, which
// a persisted index (or a shared PgVectorStore) still carries. Only a
// document a watcher makes counts (its ID is the hash of its Source
// path), so a host's own documents and files under other roots are left
// alone. An Index that cannot list its documents is not reconciled.
func (w *Watcher) reconcile(ctx context.Context, absRoots []string, seen map[string]struct{}) (err error) {
	// Same recover net as scan and upsert: the index is host-supplied
	// code on the watcher loop.
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

// underAnyRoot reports whether path lies inside one of roots, compared
// lexically: "/a/bc" is not under "/a/b".
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
	// Same recover net as scan: idx.Add is host-supplied code on the
	// watcher loop.
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
		// No longer a file to index; drop any version indexed before.
		// The document ID is the path's, so this holds for one an
		// earlier process indexed too: the walk already marked the path
		// seen, so reconcile will not remove it.
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
		if !errors.As(err, &ce) {
			return fmt.Errorf("semantic: watcher add %q: %w", path, err)
		}
		// The file's content is the fault (Latin-1 bytes, say): skip it
		// so the rest of the tree still indexes, drop any version indexed
		// before, and retry only once the file changes.
		slog.Default().Warn("semantic: watcher skipped a file it cannot chunk",
			"path", textsafe.ScrubControlBytes(path), "err", textsafe.ScrubControlBytes(ce.err.Error()))
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
