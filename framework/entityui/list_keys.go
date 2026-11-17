package entityui

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"weak"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
)

type listKeySet struct {
	mu   sync.Mutex
	seen map[string]bool
}

var (
	listKeyRegMu sync.Mutex
	listKeyReg   = map[weak.Pointer[http.Request]]*listKeySet{}
)

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
