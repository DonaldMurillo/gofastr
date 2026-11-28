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

// Watch is a project's file watch: the directory it walks and the package directories it always walks.
type Watch struct {
	dir     string
	pkg     string
	env     []string
	ctx     context.Context
	refresh sync.Mutex
	keep    atomic.Pointer[map[string]bool]
	graph   atomic.Uint64
}

// NewWatch reads pkg's package graph with go list in dir under env (nil for this process's) and returns the watch over dir.
func NewWatch(ctx context.Context, dir, pkg string, env []string) *Watch {
	w := &Watch{dir: dir, pkg: pkg, env: env, ctx: ctx}
	w.Refresh()
	return w
}

const listTimeout = 30 * time.Second

var listDeps = func(ctx context.Context, dir, pkg string, env []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
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

// Refresh re-reads the package graph; a failed query keeps the graph it had.
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

// Scan returns the mod time of every watched source and embedded-asset file.
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

// ScanModTimes is Scan with no package graph.
func ScanModTimes(dir string) map[string]time.Time {
	return (&Watch{dir: dir}).Scan()
}

// FirstSnapshot merges the scan before the first build, taken at since, with the scan after it.
func FirstSnapshot(before, after map[string]time.Time, since time.Time) map[string]time.Time {
	first := maps.Clone(before)
	for p, t := range after {
		if _, ok := before[p]; !ok && !t.After(since) {
			first[p] = t
		}
	}
	return first
}

// Poller is one poll loop over a Watch.
type Poller struct {
	w     *Watch
	prev  map[string]time.Time
	since time.Time
	graph uint64
}

// Poller starts a poll loop from before, the scan taken at since before the first build.
func (w *Watch) Poller(before map[string]time.Time, since time.Time) *Poller {
	graph := w.graph.Load()
	return &Poller{w: w, prev: FirstSnapshot(before, w.Scan(), since), since: since, graph: graph}
}

// Notify polls every 500ms until stop closes, queuing at most one reload per change.
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

// Changed scans and reports whether the tree changed since the last change it reported.
func (p *Poller) Changed() bool {
	graph := p.w.graph.Load()
	at := time.Now()
	curr := p.w.Scan()
	if p.w.graph.Load() != graph {
		return false
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

// Changed reports whether a file was added, removed or modified between prev and curr.
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
