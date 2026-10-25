package chromedptest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// browser is one running Chrome: shared by every Context call in the test
// binary with the same allocator options, or private to one test where no
// reaper can be started.
type browser struct {
	key string
	// ctx is the chromedp context that started the Chrome process;
	// cancel kills the process and waits for it to exit.
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
	dataDir     string
	// reaper and reaperIn keep Chrome from outliving the test binary;
	// see startReaper. Both are nil for a private browser.
	reaper    *exec.Cmd
	reaperIn  io.WriteCloser
	closeOnce sync.Once
}

var (
	poolMu sync.Mutex
	// pool holds the shared Chrome per allocator key.
	pool = map[string]*browser{}
	// shellPath is the sh that runs reapers. Without one, every Context
	// call gets a private Chrome, as before sharing.
	shellPath = sync.OnceValue(func() string {
		p, _ := exec.LookPath("sh")
		return p
	})
)

// reaperScript runs beside a shared Chrome. Its stdin is a pipe whose write
// end only the test binary holds, so the read returns when the binary exits
// for any reason (tests done, a panic, a -timeout kill), and the script then
// kills Chrome and deletes its profile. Nothing else would: chromedp kills
// Chrome when its context is cancelled, but a shared Chrome's context lives
// until the binary exits, and outside Linux (no parent-death signal) Chrome
// would keep running after it. The traps keep a terminal Ctrl-C, which
// reaches the whole process group, from killing the reaper first.
const reaperScript = `trap '' HUP INT TERM; read -r _; kill -9 "$1" 2>/dev/null; sleep 1; rm -rf "$2"`

// acquire returns a running Chrome for cfg: the shared one for its
// allocator options, started now if there is none or the last one died.
func acquire(t testing.TB, cfg config) (*browser, error) {
	key := allocatorKey(cfg.extra)
	if shellPath() == "" {
		b, err := launch(cfg, key, false)
		if err != nil {
			return nil, err
		}
		t.Cleanup(b.close)
		return b, nil
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	if b := pool[key]; b != nil {
		if b.alive() {
			return b, nil
		}
		delete(pool, key)
		b.close()
	}
	b, err := launch(cfg, key, true)
	if err != nil {
		return nil, err
	}
	pool[key] = b
	return b, nil
}

// allocatorKey identifies the Chrome an option set needs. The options are
// funcs, so the key is the allocator they configure, printed: its binary,
// flags, and environment. Window size and the websocket-URL wait are left
// out on purpose, since a shared Chrome serves every size and patience.
func allocatorKey(extra []chromedp.ExecAllocatorOption) string {
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), extra...)
	defer cancel()
	return fmt.Sprintf("%+v", chromedp.FromContext(ctx).Allocator)
}

// launch starts a Chrome for cfg under the startup watchdog. A reaped
// browser gets a reaper bound to the test binary's lifetime.
func launch(cfg config, key string, reap bool) (*browser, error) {
	// An explicit profile directory, rather than chromedp's own temp
	// dir, so the reaper knows what to delete.
	dir, err := os.MkdirTemp("", "chromedptest-")
	if err != nil {
		return nil, err
	}
	opts := append(slices.Clone(chromedp.DefaultExecAllocatorOptions[:]),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// Match the suite's normal-motion baseline on every host. Tests
		// for reduced motion override it explicitly through flags or CDP.
		chromedp.Flag("force-prefers-no-reduced-motion", true),
		chromedp.WSURLReadTimeout(cfg.startTimeout),
		chromedp.WindowSize(defaultWindowW, defaultWindowH),
		chromedp.UserDataDir(dir),
	)
	opts = append(opts, cfg.extra...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	b := &browser{key: key, ctx: ctx, cancel: cancel, allocCancel: allocCancel, dataDir: dir}

	// chromedp starts Chrome on the first Run, and the process lives as
	// long as the context that Run is given, so it runs on ctx itself;
	// the watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(ctx) }()
	select {
	case err = <-started:
	case <-time.After(cfg.startTimeout):
		err = fmt.Errorf("no answer within %v", cfg.startTimeout)
	}
	if err == nil && reap {
		err = b.startReaper()
	}
	if err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

// startReaper starts reaperScript watching b's Chrome process.
func (b *browser) startReaper() error {
	proc := chromedp.FromContext(b.ctx).Browser.Process()
	if proc == nil {
		return errors.New("no chrome process to reap")
	}
	cmd := exec.Command(shellPath(), "-c", reaperScript, "chromedptest-reaper", strconv.Itoa(proc.Pid), b.dataDir)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start reaper: %w", err)
	}
	b.reaper, b.reaperIn = cmd, in
	return nil
}

// lost is closed once b's Chrome has dropped its DevTools connection: it
// died, or b was closed (cancelling ctx ends the connection's read loop).
func (b *browser) lost() <-chan struct{} {
	return chromedp.FromContext(b.ctx).Browser.LostConnection
}

// alive reports whether b's Chrome still holds its DevTools connection.
func (b *browser) alive() bool {
	select {
	case <-b.lost():
		return false
	default:
		return true
	}
}

// discard drops b from the pool, so the next caller starts a fresh Chrome,
// and shuts it down.
func (b *browser) discard() {
	poolMu.Lock()
	if pool[b.key] == b {
		delete(pool, b.key)
	}
	poolMu.Unlock()
	b.close()
}

// close shuts b's Chrome down and deletes its profile. The reaper goes
// first, so it never fires on a process ID the system may reuse.
func (b *browser) close() {
	b.closeOnce.Do(func() {
		if b.reaper != nil {
			_ = b.reaper.Process.Kill()
			_ = b.reaper.Wait()
		}
		b.cancel()
		b.allocCancel()
		_ = os.RemoveAll(b.dataDir)
	})
}
