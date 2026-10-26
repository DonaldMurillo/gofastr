package chromedptest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// browser is one running Chrome: shared by every Context call in the test
// binary with the same allocator options, or private to one window.
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
	reaper     *exec.Cmd
	reaperIn   io.WriteCloser
	disarmOnce sync.Once
	closeOnce  sync.Once
	// windows counts the Context windows holding b, from acquire to
	// release. retired means b has left the pool (it died, failed, or
	// was never pooled) and closes once windows reaches zero, so a
	// failure in one test never kills a window another test still
	// drives. Both are guarded by poolMu.
	windows int
	retired bool
}

var (
	poolMu sync.Mutex
	// pool holds the shared Chrome per allocator key.
	pool = map[string]*browser{}
	// shellPath is the sh that runs reapers; see reaperShell.
	shellPath = sync.OnceValue(func() string { return reaperShell(runtime.GOOS) })
)

// reaperShell finds the sh that runs reapers on goos. Without one, every
// Context call gets a private Chrome, as before sharing. Windows always goes
// without: Git's sh.exe is found there, but its kill cannot signal a native
// Windows process, so a shared Chrome would outlive every test binary.
func reaperShell(goos string) string {
	if goos == "windows" {
		return ""
	}
	p, _ := exec.LookPath("sh")
	return p
}

// reaperScript runs beside a shared Chrome. Its stdin is a pipe whose write
// end only the test binary holds, so the read returns when the binary exits
// for any reason (tests done, a panic, a -timeout kill), and the script then
// kills Chrome and deletes its profile. Nothing else would: chromedp kills
// Chrome when its context is cancelled, but a shared Chrome's context lives
// until the binary exits, and outside Linux (no parent-death signal) Chrome
// would keep running after it. The traps keep a terminal Ctrl-C, which
// reaches the whole process group, from killing the reaper first.
const reaperScript = `trap '' HUP INT TERM; read -r _; kill -9 "$1" 2>/dev/null; sleep 1; rm -rf "$2"`

// acquire returns a running Chrome for cfg and counts one window on it; the
// caller hands it back with release. A shareable option set gets the shared
// Chrome for its key, started now if there is none or the last one died;
// anything else gets a private Chrome that closes with its one window.
func acquire(cfg config) (*browser, error) {
	key, shareable := allocatorKey(cfg.extra)
	if !shareable || shellPath() == "" {
		b, err := launch(cfg, key, false)
		if err != nil {
			return nil, err
		}
		b.windows, b.retired = 1, true
		return b, nil
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	return acquireLocked(cfg, key)
}

// acquireLocked is acquire's pooled path for key. poolMu must be held.
func acquireLocked(cfg config, key string) (*browser, error) {
	if b := pool[key]; b != nil {
		if b.alive() {
			b.windows++
			return b, nil
		}
		if b.retireLocked() {
			b.close()
		}
	}
	b, err := launch(cfg, key, true)
	if err != nil {
		return nil, err
	}
	// A Chrome that dies idle in the pool leaves the pool at once, and
	// its reaper is disarmed before the system can reuse its process ID.
	// chromedp has already cancelled the process by then.
	go func() {
		<-b.lost()
		b.disarm()
		b.retire()
	}()
	b.windows = 1
	pool[key] = b
	return b, nil
}

// allocatorKey identifies the Chrome an option set needs. The options are
// funcs, so the key is the allocator they configure, printed: its binary,
// flags, and environment. Window size and the websocket-URL wait are left
// out on purpose, since a shared Chrome serves every size and patience.
//
// shareable is false when the allocator holds a func, writer, or other
// reference (ModifyCmdFunc, CombinedOutput): printing shows only its
// address or innards, so two closures over different values would look the
// same and a per-test writer would never look the same twice.
func allocatorKey(extra []chromedp.ExecAllocatorOption) (key string, shareable bool) {
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), extra...)
	defer cancel()
	alloc := chromedp.FromContext(ctx).Allocator
	v := reflect.ValueOf(alloc).Elem()
	for i := range v.NumField() {
		switch f := v.Field(i); f.Kind() {
		case reflect.Func, reflect.Interface, reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
			if !f.IsNil() {
				return "", false
			}
		}
	}
	return fmt.Sprintf("%+v", alloc), true
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
	// The websocket dial after Chrome prints its URL has its own 10s
	// default in chromedp; a cold Chrome on a loaded runner can take
	// longer to answer it, so it gets the same startup allowance.
	ctx, cancel := chromedp.NewContext(allocCtx,
		chromedp.WithBrowserOption(chromedp.WithDialTimeout(cfg.startTimeout)))
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

// disarm stops b's reaper, so it never fires on a process ID the system may
// reuse once Chrome has exited.
func (b *browser) disarm() {
	b.disarmOnce.Do(func() {
		if b.reaper != nil {
			_ = b.reaper.Process.Kill()
			_ = b.reaper.Wait()
		}
	})
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

// retireLocked takes b out of the pool, so the next caller starts a fresh
// Chrome, and reports whether b should close now: no window holds it.
// poolMu must be held.
func (b *browser) retireLocked() bool {
	if pool[b.key] == b {
		delete(pool, b.key)
	}
	b.retired = true
	return b.windows == 0
}

// retire takes b out of the pool and closes it once no window holds it.
func (b *browser) retire() {
	poolMu.Lock()
	closeNow := b.retireLocked()
	poolMu.Unlock()
	if closeNow {
		b.close()
	}
}

// release hands back one window acquire counted, closing a retired b when
// it was the last.
func (b *browser) release() {
	poolMu.Lock()
	b.windows--
	closeNow := b.retired && b.windows == 0
	poolMu.Unlock()
	if closeNow {
		b.close()
	}
}

// close shuts b's Chrome down and deletes its profile. The reaper goes
// first, so it never fires on a process ID the system may reuse.
func (b *browser) close() {
	b.closeOnce.Do(func() {
		b.disarm()
		b.cancel()
		b.allocCancel()
		_ = os.RemoveAll(b.dataDir)
	})
}
