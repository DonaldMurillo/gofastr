//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

// WebKitGTK API4.1 keeps the deprecated WebSQL/plugin bits in this enum.
// Its service-worker flag is 1<<12, NOT the 1<<10 used by API 6.0.
const profileServiceWorkers = 1 << 12

func (s *linuxShell) createWebContext(ctx context.Context) (uintptr, error) {
	if s.profile == nil {
		result := call(gtk.Sym.WebkitWebContextNewEphemeral)
		if result == 0 {
			return 0, errors.New("desktop/linux: create ephemeral WebKit context")
		}
		return result, nil
	}
	p := s.profile
	if p.addr == "127.0.0.1:0" || p.lock == nil {
		return 0, errors.New("desktop/linux: persistent browser origin has not been committed")
	}
	var manager uintptr
	withString("base-data-directory", func(dataKey uintptr) {
		withString(filepath.Join(p.dir, "data"), func(data uintptr) {
			withString("base-cache-directory", func(cacheKey uintptr) {
				withString(filepath.Join(p.dir, "cache"), func(cache uintptr) {
					manager = call(gtk.Sym.WebkitWebsiteDataManagerNew, dataKey, data, cacheKey, cache, 0)
				})
			})
		})
	})
	if manager == 0 {
		return 0, errors.New("desktop/linux: create persistent WebKit data manager")
	}
	defer call(gtk.Sym.GObjectUnref, manager)
	// A browser profile is not consent to storing HTTP-auth passwords in a
	// desktop credential store. Cookies/website storage are the opt-in surface.
	call(gtk.Sym.WebkitDataManagerSetPersistentCredentials, manager, 0)
	cookies := call(gtk.Sym.WebkitDataManagerGetCookieManager, manager)
	if cookies == 0 {
		return 0, errors.New("desktop/linux: get persistent cookie manager")
	}
	withString(filepath.Join(p.dir, "cookies.sqlite"), func(path uintptr) {
		// SQLite=1. This setter returns void: success here is configuration,
		// NOT durable-write acknowledgement. Only non-session cookies persist.
		call(gtk.Sym.WebkitCookieManagerSetPersistentStorage, cookies, path, 1)
	})
	result := call(gtk.Sym.WebkitWebContextNewWithDataManager, manager)
	if result == 0 {
		return 0, errors.New("desktop/linux: create persistent WebKit context")
	}
	p.native = true
	// A persisted worker could intercept the fresh /enter handshake before
	// the real app answers. Clear registrations, await native completion, and
	// fail closed BEFORE creating any view. Storage/cookies are not cleared.
	if err := clearProfileWorkers(ctx, manager); err != nil {
		call(gtk.Sym.GObjectUnref, result)
		return 0, err
	}
	return result, nil
}

var profileClearCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	p := new(uintptr)
	var pin runtime.Pinner
	pin.Pin(p)
	defer pin.Unpin()
	ok := call(gtk.Sym.WebkitDataManagerClearFinish, a.Int[0], a.Int[1], uintptr(unsafe.Pointer(p)))
	err := takeGError(*p)
	if ok == 0 && err == nil {
		err = errors.New("native data clear failed")
	}
	publishResult(a.Int[2], webResult{err: err})
	return 0
})

// Run on GTK's thread before gtk_main. The asynchronous callback has a fixed
// address and token lookup, so cancellation never leaves a Go stack pointer
// with C and a late completion still consumes its GError/result safely.
func clearProfileWorkers(ctx context.Context, manager uintptr) error {
	token := uintptr(asyncSequence.Add(1))
	ch := make(chan webResult, 1)
	asyncResults.Store(token, ch)
	defer asyncResults.Delete(token)
	cancel := call(gtk.Sym.GCancellableNew)
	if cancel == 0 {
		return errors.New("desktop/linux: allocate profile startup cancellation")
	}
	defer call(gtk.Sym.GObjectUnref, cancel)
	call(gtk.Sym.WebkitDataManagerClear, manager, profileServiceWorkers, 0, cancel, profileClearCallback, token)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		call(gtk.Sym.GMainContextIteration, 0, 0)
		select {
		case result := <-ch:
			if result.err != nil {
				return fmt.Errorf("desktop/linux: clear persisted service workers before boot: %w", result.err)
			}
			return nil
		case <-ctx.Done():
			call(gtk.Sym.GCancellableCancel, cancel)
			return ctx.Err()
		case <-deadline.C:
			call(gtk.Sym.GCancellableCancel, cancel)
			return errors.New("desktop/linux: timed out clearing persisted service workers; refusing profile startup")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
