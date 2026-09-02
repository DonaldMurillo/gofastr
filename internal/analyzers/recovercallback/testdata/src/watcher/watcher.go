// Package watcher is a NOVEL instantiation of the shape: a filesystem
// watcher (no such code exists in this repo) fanning events into
// handler fields from a goroutine, plus the postures the rule keeps
// quiet on (http.Handler-shaped callbacks, CancelFunc fields, and
// test files).
package watcher

import (
	"context"
	"net/http"
	"time"
)

type Watcher struct {
	onCreate func(path string)
	onDelete func(path string)
	cancel   context.CancelFunc
}

// spawnBad launches a goroutine that calls handler fields with no
// recover — the goroutine is the whole dispatch path.
func (w *Watcher) spawnBad(paths <-chan string) {
	go func() {
		for p := range paths {
			if w.onCreate != nil {
				w.onCreate(p) // want `recovercallback: w\.onCreate is invoked with no recover in scope`
			}
			w.onDelete(p) // want `recovercallback: w\.onDelete is invoked with no recover in scope`
		}
	}()
}

// spawnGood recovers around the same fan-out.
func (w *Watcher) spawnGood(paths <-chan string) {
	go func() {
		defer func() {
			_ = recover()
		}()
		for p := range paths {
			if w.onCreate != nil {
				w.onCreate(p)
			}
		}
	}()
}

// httpHandlerCallback is quiet: net/http recovers handler panics per
// request, so a HandlerFunc-shaped field is not this rule's business
// even inside a loop.
type site struct {
	next http.HandlerFunc
}

func (s *site) loop(reqs <-chan *http.Request, w http.ResponseWriter) {
	for r := range reqs {
		s.next(w, r)
	}
}

// cancelFunc is quiet: CancelFunc never runs user code.
func (w *Watcher) cancelFunc() {
	time.AfterFunc(time.Second, func() {
		w.cancel()
	})
}
