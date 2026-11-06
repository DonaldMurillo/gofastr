package devcmd

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestDevWatchRunsTrailingSave(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "main.go")
	if err := os.WriteFile(source, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	watch := NewDevContractWatch(root)
	analyzer := contracts.Analyzers()[0]
	original := analyzer.Run
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var runs atomic.Int32
	observed := make(chan string, 4)
	analyzer.Run = func(pass *contracts.Pass) ([]contracts.Diagnostic, error) {
		if pass.Root != root {
			return original(pass)
		}
		content, err := os.ReadFile(source)
		if runs.Add(1) == 1 {
			close(started)
			<-release
		}
		observed <- string(content)
		return nil, err
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		waitWatchIdle(t, watch)
		analyzer.Run = original
	})
	watch.Run()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("analysis never started")
	}
	latest := "package main\nvar latest = true\n"
	if err := os.WriteFile(source, []byte(latest), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		watch.Run()
	}
	releaseOnce.Do(func() { close(release) })
	waitWatchIdle(t, watch)
	if runs.Load() != 2 {
		t.Fatalf("burst ran %d analyses, want the in-flight pass and one trailing pass", runs.Load())
	}
	<-observed
	if got := <-observed; got != latest {
		t.Fatalf("trailing pass read %q, want latest save %q", got, latest)
	}
}
