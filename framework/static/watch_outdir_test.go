package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestWatchIgnoresItsOwnOutput: Watch polls every file under watchDirs,
// and OutDir under one of them (watch ".", write "./dist") is the common
// layout. Each rebuild rewrites every export file with a new mtime, so
// the next poll saw them all as changed and rebuilt again, forever: one
// source edit became a loop that never settled. The build's own output is
// not a source.
func TestWatchIgnoresItsOwnOutput(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "static")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(src, "app.css")
	if err := os.WriteFile(asset, []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := coreapp.NewApp("Watch Loop")
	app.Register("/home", &homeScreen{}, nil)

	var rebuilds atomic.Int32
	b := &Builder{
		Host:   uihost.New(app, uihost.WithStaticDir(src)),
		OutDir: filepath.Join(base, "dist"),
		Logger: func(format string, _ ...any) {
			if strings.HasPrefix(format, "change detected") {
				rebuilds.Add(1)
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Watch(ctx, []string{base}, 20*time.Millisecond, func(err error) { t.Error(err) }) }()

	// Edit one source until a rebuild starts: the initial build's
	// snapshot may land after an edit made too early, which then reads
	// as the baseline.
	deadline := time.Now().Add(15 * time.Second)
	for i := 1; rebuilds.Load() == 0; i++ {
		if time.Now().After(deadline) {
			t.Fatal("no rebuild after editing a watched source")
		}
		stamp := time.Now().Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(asset, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	// No source changes from here on: the count must hold still.
	time.Sleep(500 * time.Millisecond)
	settled := rebuilds.Load()
	time.Sleep(500 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := rebuilds.Load(); n != settled {
		t.Fatalf("rebuilds kept coming with no source edit (%d, then %d): Watch is reading its own OutDir as a source", settled, n)
	}
}
