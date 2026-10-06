package entityui

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"weak"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A page with two unkeyed lists, or two lists sharing a key, is refused:
// the second list with a taken key fails its slot. Both lists would read
// and write the same query params, so one silently drives the other.
//
// Detection is per request: every list rendered for one *http.Request
// claims its key in a registry keyed weakly by that request. The weak
// key keeps the registry from growing: when the request is collected
// after its response, the runtime cleanup drops its entry, so nothing
// accumulates across a long-lived process and no end-of-render hook is
// needed. Two apps in one process stay apart — their lists render in
// different requests — except when one app's surface is embedded in
// another's page render, where a shared key would genuinely collide.
//
// A render with no live request (a build-time or direct render, and the
// tests that drive one) cannot share state with anything, so every key
// is allowed there.

// listKeySet is the set of keys claimed by one request's rendered lists.
type listKeySet struct {
	mu   sync.Mutex
	seen map[string]bool
}

var (
	listKeyRegMu sync.Mutex
	listKeyReg   = map[weak.Pointer[http.Request]]*listKeySet{}
)

// claimListKey records that a list with key rendered for ctx's request
// and reports whether the key was still free. The second claim of one
// key, from either list of a duplicated pair, returns false.
func claimListKey(ctx context.Context, key string) bool {
	r := appui.RequestFromContext(ctx)
	if r == nil {
		return true
	}
	wp := weak.Make(r)
	listKeyRegMu.Lock()
	set, ok := listKeyReg[wp]
	if !ok {
		set = &listKeySet{seen: map[string]bool{}}
		listKeyReg[wp] = set
		runtime.AddCleanup(r, func(p weak.Pointer[http.Request]) {
			listKeyRegMu.Lock()
			delete(listKeyReg, p)
			listKeyRegMu.Unlock()
		}, wp)
	}
	listKeyRegMu.Unlock()

	set.mu.Lock()
	defer set.mu.Unlock()
	if set.seen[key] {
		return false
	}
	set.seen[key] = true
	return true
}
