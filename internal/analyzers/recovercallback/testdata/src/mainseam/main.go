// Package main mirrors examples/site's live dashboard: a command declares
// an interface as a seam over its own wiring (dashIslandPusher over the
// framework's *island.Manager) and calls it from a ticker goroutine. A
// command hands no extension point to a host, it is the host, so the
// interface leg stays quiet. A func-typed registry field still fires: the
// leg that is quiet is the interface one only.
package main

import "time"

type pusher interface {
	PushUpdate(id string, html string)
}

type hooks struct {
	OnTick func(n int)
}

func run(p pusher, h hooks) {
	t := time.NewTicker(time.Second)
	go func() {
		for range t.C {
			p.PushUpdate("stats", "<b>1</b>") // quiet: a command's own seam
			h.OnTick(1)                       // want `recovercallback: h\.OnTick is invoked with no recover in scope`
		}
	}()
}

func main() {}
