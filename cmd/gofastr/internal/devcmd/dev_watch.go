package devcmd

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Watch is the dev watcher's view of a project: the directory it walks
// and the directories it must walk whatever their name, the build
// target's own package and every package it imports from the tree.
type Watch struct {
	dir string
	pkg string
	// env is the build's environment, nil for this process's: the
	// graph go list reports depends on it (CGO_ENABLED, GOOS and build
	// tags pick files, and files pick imports).
	env []string
	// ctx ends a dependency query in progress: shutdown must not wait
	// on a go list stalled on a module proxy.
	ctx context.Context
	// refresh serializes Refresh, so a slower, older query can never
	// land after a newer one and put a stale graph back.
	refresh sync.Mutex
	// keep holds slash paths relative to dir: each package directory in
	// the target's graph and every directory above it. Nil keeps none.
	keep atomic.Pointer[map[string]bool]
	// graph counts the keep sets stored that differ from the one before,
	// so a Poller can tell a graph change from a save.
	graph atomic.Uint64
}

// NewWatch reads pkg's package graph (go list, run in dir with env, the
// build's environment; nil inherits this process's) and returns the
// watch over dir. Cancelling ctx ends a query in progress, this one and
// every later Refresh: the watch then keeps the graph it has.
func NewWatch(ctx context.Context, dir, pkg string, env []string) *Watch {
	w := &Watch{dir: dir, pkg: pkg, env: env, ctx: ctx}
	w.Refresh()
	return w
}

// listTimeout bounds the go list behind a Refresh.
const listTimeout = 30 * time.Second

// listDeps lists the directories of pkg's non-standard dependencies,
// pkg included. A variable so a test can hold one query back.
var listDeps = func(ctx context.Context, dir, pkg string, env []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	// No -e: a package that does not load fails the query, and a failed
	// query keeps the last graph rather than a partial one.
	list := exec.CommandContext(ctx, "go", "list", "-buildvcs=false", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", pkg)
	list.Dir = dir
	list.Env = env
	list.WaitDelay = 5 * time.Second
	out, err := list.Output()
	if err != nil {
		return nil, err
	}
	var dirs []string
	for line := range strings.Lines(string(out)) {
		if d := strings.TrimSpace(line); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs, nil
}

// Refresh re-reads the package graph, after a rebuild that may have
// added or dropped an import. A successful query replaces the graph, so
// an import that is gone stops keeping its directory. A failed one (a
// save that left go.mod or an import broken, or a cancelled ctx) keeps
// the graph it had: a package does not drop out of the watch because
// the tree is mid-edit. Calls run one at a time, in call order.
func (w *Watch) Refresh() {
	w.refresh.Lock()
	defer w.refresh.Unlock()
	root, err := filepath.Abs(w.dir)
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	keep := map[string]bool{}
	add := func(d string) {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			d = real
		}
		rel, err := filepath.Rel(root, d)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
		for rel != "." {
			keep[filepath.ToSlash(rel)] = true
			rel = filepath.Dir(rel)
		}
	}
	// The target's own directory counts even when go list cannot run.
	if strings.HasPrefix(w.pkg, ".") {
		add(filepath.Join(root, filepath.FromSlash(w.pkg)))
	}
	dirs, err := listDeps(w.ctx, w.dir, w.pkg, w.env)
	if err != nil {
		if w.keep.Load() == nil {
			w.keep.Store(&keep)
		}
		return
	}
	for _, d := range dirs {
		add(d)
	}
	if was := w.keep.Swap(&keep); was == nil || !maps.Equal(*was, keep) {
		w.graph.Add(1)
	}
}

// skipDir reports a directory the watcher does not walk: dependencies
// (vendor, node_modules) and every dot-directory (.git, .gofastr,
// editor state, and agent worktrees under .claude/worktrees, whose
// saves would otherwise restart this server) at any depth, and build
// output (dist, gen, tmp) at the root alone. A directory the build
// target's package graph runs through is always walked.
func (w *Watch) skipDir(root, path, name string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if keep := w.keep.Load(); keep != nil && (*keep)[rel] {
		return false
	}
	switch name {
	case "vendor", "node_modules":
		return true
	case "dist", "gen", "tmp":
		if !strings.Contains(rel, "/") {
			return true
		}
	}
	return strings.HasPrefix(name, ".")
}

// Scan walks the directory and records the latest mod time of source
// and embedded-asset files. Go embeds .js, .css, .html, and .md at build
// time (framework docs, llm.md sources), so changes to those files also
// require a rebuild.
func (w *Watch) Scan() map[string]time.Time {
	result := make(map[string]time.Time)
	root := w.dir
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// The root itself is walked whatever its name (".", a dot-dir
		// the developer pointed --dir at).
		if info.IsDir() && path != root && w.skipDir(root, path, info.Name()) {
			return filepath.SkipDir
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".js", ".css", ".html", ".md":
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Stat(path)
				if err != nil || target.IsDir() {
					return nil
				}
				info = target
			}
			result[path] = info.ModTime()
		}
		return nil
	})
	return result
}

// ScanModTimes scans dir with no package graph: root-level build output
// and dependency and dot-directories are skipped, everything else
// walked. The dev loops scan through NewWatch, which also keeps every
// directory the build target imports from.
func ScanModTimes(dir string) map[string]time.Time {
	return (&Watch{dir: dir}).Scan()
}

// FirstSnapshot is a watch's first snapshot. before is the scan taken
// before the first build (ScanModTimes, no package graph yet), started
// at since, and after the watch's scan once the build ran and the graph
// loaded. A file in both keeps its time from before, and a file only in
// before stays, so a save or a removal made during the build or the
// graph read differs from the next scan and rebuilds. A file only in
// after (a gen/ file the graph keeps, a file created since) counts by
// its time: one not modified since the scan began is the baseline, and
// a newer one is left out, so the next scan sees it and rebuilds.
func FirstSnapshot(before, after map[string]time.Time, since time.Time) map[string]time.Time {
	first := maps.Clone(before)
	for p, t := range after {
		if _, ok := before[p]; !ok && !t.After(since) {
			first[p] = t
		}
	}
	return first
}

// Poller is a poll loop's view of a watch: the last snapshot, the time
// the scan behind it began, and the graph it was taken under.
type Poller struct {
	w     *Watch
	prev  map[string]time.Time
	since time.Time
	graph uint64
}

// Poller starts a poll loop from before, the scan taken at since before
// the first build (FirstSnapshot).
func (w *Watch) Poller(before map[string]time.Time, since time.Time) *Poller {
	graph := w.graph.Load()
	return &Poller{w: w, prev: FirstSnapshot(before, w.Scan(), since), since: since, graph: graph}
}

// Notify polls the watch every 500ms until stop closes and queues a
// reload for each change; a reload already queued absorbs the next.
func (w *Watch) Notify(before map[string]time.Time, since time.Time, stop <-chan struct{}, reload chan<- struct{}) {
	poll := w.Poller(before, since)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if poll.Changed() {
				select {
				case reload <- struct{}{}:
				default:
				}
			}
		}
	}
}

// Changed scans the watch and reports whether the tree changed since the
// last scan that did. A graph change in between (a Refresh after a save
// added or dropped an import) moves files into or out of the walk, which
// is not a save: a file it brings in that is no newer than the last
// change's scan joins the snapshot, and one it drops that still exists
// leaves it, the way FirstSnapshot treats the first graph. Without that
// the first import of a root gen/ package restarted the app twice.
func (p *Poller) Changed() bool {
	graph := p.w.graph.Load()
	at := time.Now()
	curr := p.w.Scan()
	if p.w.graph.Load() != graph {
		return false // the graph moved mid-walk: the next tick rescans
	}
	if graph != p.graph {
		p.prev = rebase(p.prev, curr, p.since)
		p.graph = graph
	}
	if !Changed(p.prev, curr) {
		return false
	}
	p.prev, p.since = curr, at
	return true
}

// rebase folds a graph change into prev: a file only in curr whose time
// is not after since is baseline, and a file only in prev that is still
// on disk left the walk. A file only in curr that is newer, or one only
// in prev that is gone, stays a change.
func rebase(prev, curr map[string]time.Time, since time.Time) map[string]time.Time {
	next := maps.Clone(prev)
	for p, t := range curr {
		if _, ok := prev[p]; !ok && !t.After(since) {
			next[p] = t
		}
	}
	for p := range prev {
		if _, ok := curr[p]; !ok {
			if _, err := os.Lstat(p); err == nil {
				delete(next, p)
			}
		}
	}
	return next
}

// Changed compares two mod-time maps and returns true if any file was added, removed, or modified.
func Changed(prev, curr map[string]time.Time) bool {
	if len(prev) != len(curr) {
		return true
	}
	for path, t := range curr {
		if pt, ok := prev[path]; !ok || pt != t {
			return true
		}
	}
	return false
}
