package devcmd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// Two refreshes ran at once and the older, slower query landed last,
// putting back a graph from before the newer save. Refreshes run one at
// a time, so the last call's graph is the one that stands.
func TestRefreshKeepsNewestGraph(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"gen/a.go": "package gen\n", "main.go": "package main\n"})
	release := make(chan struct{})
	var calls int
	var mu sync.Mutex
	prev := listDeps
	listDeps = func(context.Context, string, string, []string) ([]string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1: // NewWatch: no gen
			return []string{root}, nil
		case 2: // the older refresh: still no gen, and slow
			<-release
			return []string{root}, nil
		default: // the newer save imports gen
			return []string{root, filepath.Join(root, "gen")}, nil
		}
	}
	t.Cleanup(func() { listDeps = prev })
	w := NewWatch(context.Background(), root, ".", nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); w.Refresh() }() // the older query
	time.Sleep(50 * time.Millisecond)
	go func() { defer wg.Done(); w.Refresh() }() // the newer one
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := watched(root, w); !slices.Contains(got, "gen/a.go") {
		t.Fatalf("watched %v: an older graph overwrote the newer one", got)
	}
}

// Old graph entries were copied into every refresh, so an import the
// app dropped kept its root-level output directory watched for good.
func TestRefreshDropsRemovedImport(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/w\n\ngo 1.22\n",
		"main.go":    "package main\n\nimport _ \"example.com/w/gen\"\n\nfunc main() {}\n",
		"gen/api.go": "package gen\n",
	})
	w := NewWatch(context.Background(), root, ".", nil)
	if got := watched(root, w); !slices.Contains(got, "gen/api.go") {
		t.Fatalf("watched %v; the imported gen/ is missing", got)
	}
	writeTree(t, root, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	w.Refresh()
	if got := watched(root, w); slices.Contains(got, "gen/api.go") {
		t.Fatalf("watched %v: gen/ stayed watched after main dropped the import", got)
	}
}

// A SIGTERM during the first build with a stalled module proxy still
// sat in the watcher's go list for its full 30 s timeout. The query
// runs on the shutdown context and ends with it.
func TestWatchQueryEndsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var held []net.Conn
	var mu sync.Mutex
	go func() { // a proxy that accepts and never answers
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":  "module example.com/w\n\ngo 1.22\n\nrequire example.invalid/stall v1.0.0\n",
		"main.go": "package main\n\nimport _ \"example.invalid/stall\"\n\nfunc main() {}\n",
	})
	// The process env, the way a user's stalled GOPROXY reaches it.
	for k, v := range map[string]string{"GOPROXY": "http://" + ln.Addr().String(), "GOFLAGS": "-mod=mod",
		"GOSUMDB": "off", "GONOSUMDB": "", "GOPRIVATE": "", "GONOPROXY": "", "GOWORK": "off"} {
		t.Setenv(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	NewWatch(ctx, root, ".", nil)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("the dependency query ran %v past its cancelled context", took)
	}
}

// Desktop builds run with CGO_ENABLED=0 while the watcher's go list
// inherited cgo on, and build tags pick files, and files imports: the
// watcher could keep the wrong graph. It lists with the build's env.
func TestWatchUsesBuildEnv(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/w\n\ngo 1.22\n",
		"main.go":    "package main\n\nfunc main() {}\n",
		"nocgo.go":   "//go:build !cgo\n\npackage main\n\nimport _ \"example.com/w/gen\"\n",
		"gen/api.go": "package gen\n",
	})
	off := NewWatch(context.Background(), root, ".", append(os.Environ(), "CGO_ENABLED=0"))
	if got := watched(root, off); !slices.Contains(got, "gen/api.go") {
		t.Fatalf("with the build's CGO_ENABLED=0, watched %v; gen/ is imported there", got)
	}
	on := NewWatch(context.Background(), root, ".", append(os.Environ(), "CGO_ENABLED=1"))
	if got := watched(root, on); slices.Contains(got, "gen/api.go") {
		t.Fatalf("with CGO_ENABLED=1, watched %v; gen/ is not imported there", got)
	}
}

// A save that added the first import of a root gen/ package restarted
// the app twice: after the reload's Refresh, the next scan saw gen/'s
// files for the first time and counted them as a change. Files a graph
// change brings into (or takes out of) the walk are the new baseline;
// a later save in gen/ still rebuilds.
func TestRefreshIsNotAChange(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"main.go": "package main\n", "gen/api.go": "package gen\n"})
	old := time.Now().Add(-time.Hour)
	for _, f := range []string{"main.go", "gen/api.go"} {
		if err := os.Chtimes(filepath.Join(root, f), old, old); err != nil {
			t.Fatal(err)
		}
	}
	var graph []string
	var mu sync.Mutex
	prev := listDeps
	listDeps = func(context.Context, string, string, []string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		return graph, nil
	}
	t.Cleanup(func() { listDeps = prev })
	setGraph := func(dirs ...string) { mu.Lock(); graph = dirs; mu.Unlock() }
	touch := func(f string, at time.Time) {
		t.Helper()
		if err := os.Chtimes(filepath.Join(root, f), at, at); err != nil {
			t.Fatal(err)
		}
	}

	setGraph(root)
	before := ScanModTimes(root)
	w := NewWatch(context.Background(), root, ".", nil)
	poll := w.Poller(before, time.Now())
	if poll.Changed() {
		t.Fatal("an untouched tree changed")
	}
	touch("main.go", time.Now()) // the save that adds the import
	if !poll.Changed() {
		t.Fatal("the save did not change")
	}
	setGraph(root, filepath.Join(root, "gen"))
	w.Refresh() // after the reload
	if poll.Changed() {
		t.Fatal("gen/ coming into the walk restarted the app a second time")
	}
	touch("gen/api.go", time.Now().Add(time.Second))
	if !poll.Changed() {
		t.Fatal("a save in the now-watched gen/ did not change")
	}
	setGraph(root)
	w.Refresh() // the import is gone again
	if poll.Changed() {
		t.Fatal("gen/ leaving the walk restarted the app")
	}
}
