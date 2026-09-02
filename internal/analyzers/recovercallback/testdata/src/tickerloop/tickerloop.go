// Package tickerloop pins the timer-dispatch leg (review finding R4):
// a bare receive whose channel expression is a .C selector on a
// *time.Ticker or *time.Timer is a timer-driven dispatch loop — the
// callback panic kills the process exactly as a read-loop panic would.
// A bare receive on an ordinary channel stays a coordination wait.
package tickerloop

import "time"

type Worker struct {
	t   *time.Ticker
	tm  *time.Timer
	sub func(ev string)

	ready chan struct{}
	fin   func()
}

// runTicker dispatches on every tick with no net. Fires.
func (w *Worker) runTicker() {
	for {
		<-w.t.C
		w.sub("tick") // want `recovercallback: w\.sub is invoked with no recover in scope`
	}
}

// runTimer: the one-shot Timer spelling. Fires.
func (w *Worker) runTimer() {
	for {
		<-w.tm.C
		w.sub("tock") // want `recovercallback: w\.sub is invoked with no recover in scope`
	}
}

// coordination is quiet: a bare receive on a plain channel inside a
// loop is a coordination wait (the cached resolver's singleflight),
// not a dispatch loop.
func (w *Worker) coordination() {
	for {
		<-w.ready
		w.fin()
	}
}
