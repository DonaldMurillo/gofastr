//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

// GTK and WebKitGTK bind to the thread that first initializes them.
// WebKit records its main thread on first use and checks it for the rest
// of the process, and GTK's default main context runs on whichever thread
// iterates it. A shell lifetime therefore cannot lock the calling
// goroutine's thread and unlock it on return: the next Run would land on
// a different thread, so before #605 a second Run was refused.
//
// The toolkit thread is started once, locked for the life of the process,
// and every shell lifetime runs its GTK work on it in turn. Its goroutine
// never returns while it is healthy, so the OS thread is never handed back
// to the scheduler.
var toolkitThread struct {
	once sync.Once
	work chan func()
	tid  atomic.Int64
	lost atomic.Bool
}

var errToolkitThreadLost = errors.New("desktop/linux: the GTK thread exited (runtime.Goexit in a shell callback); GTK cannot restart in this process")

// onToolkitThread runs fn on the toolkit thread and waits for it. A call
// already on that thread runs fn inline. A panic in fn is re-raised on the
// caller's goroutine, so a recover around Run still sees it. If fn calls
// runtime.Goexit, the toolkit thread is gone for good: this call and every
// later one return errToolkitThreadLost.
func onToolkitThread(fn func()) error {
	toolkitThread.once.Do(func() {
		toolkitThread.work = make(chan func())
		started := make(chan struct{})
		go func() {
			// Never unlocked. If this goroutine ever exits, the runtime
			// terminates the thread with it.
			runtime.LockOSThread()
			toolkitThread.tid.Store(int64(syscall.Gettid()))
			close(started)
			for f := range toolkitThread.work {
				f()
			}
		}()
		<-started
	})
	if toolkitThread.tid.Load() == int64(syscall.Gettid()) {
		fn()
		return nil
	}
	if toolkitThread.lost.Load() {
		return errToolkitThreadLost
	}
	var (
		recovered any
		panicked  bool
		finished  bool
	)
	done := make(chan struct{})
	toolkitThread.work <- func() {
		defer close(done)
		defer func() {
			if finished {
				return
			}
			if recovered = recover(); recovered != nil {
				panicked = true
				return
			}
			// Neither returned nor panicked: fn called runtime.Goexit,
			// which is unwinding the toolkit goroutine right now.
			toolkitThread.lost.Store(true)
		}()
		fn()
		finished = true
	}
	<-done
	switch {
	case panicked:
		panic(recovered)
	case !finished:
		return errToolkitThreadLost
	}
	return nil
}
