package crossguard

import (
	"callbackguard"
	"context"
)

type Worker struct{ Callback func() }

func (worker *Worker) Start() {
	go worker.guarded()
	go worker.unguarded()
	go worker.nested()
	go worker.wrapped()
	go worker.spawned()
	go worker.conditional()
	go worker.rethrown()
	go worker.assigned()
	go worker.deferredPanic()
	go worker.literalPanic()
	go worker.shortCircuit()
	go worker.earlyReturn()
}

func (worker *Worker) shortCircuit() {
	defer callbackguard.ShortCircuit(false)
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) earlyReturn() {
	defer callbackguard.EarlyReturn(true)
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) deferredPanic() {
	defer callbackguard.DeferredPanic()
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) literalPanic() {
	defer callbackguard.LiteralPanic()
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) conditional() {
	defer callbackguard.Conditional(false)
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) rethrown() {
	defer callbackguard.Rethrow()
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) assigned() {
	defer callbackguard.Assigned()
	worker.Callback()
}

func (worker *Worker) guarded() {
	defer callbackguard.LogPanic(context.Background(), "callback panicked")
	worker.Callback()
}

func (worker *Worker) unguarded() {
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) nested() {
	defer callbackguard.NestedGuard()
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) wrapped() {
	defer callbackguard.WrappedGuard()
	worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}

func (worker *Worker) spawned() {
	defer callbackguard.LogPanic(context.Background(), "wrong goroutine")
	go worker.Callback() // want `recovercallback: worker.Callback is invoked with no recover in scope`
}
