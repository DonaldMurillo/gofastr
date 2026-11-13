package atomicslot

import (
	"callbackguard"
	"context"
	"sync/atomic"
	"time"
)

var observer atomic.Pointer[func()]
var internal atomic.Pointer[func()]
var cancelFn atomic.Pointer[func()]

func SetObserver(callback func()) { observer.Store(&callback) }

func SetInternal() {
	callback := func() {}
	internal.Store(&callback)
}

func SetCancel(callback func()) { cancelFn.Store(&callback) }

func Run(ticker *time.Ticker) {
	for range ticker.C {
		if callback := observer.Load(); callback != nil {
			(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
		}
	}
}

func RunGuarded(ticker *time.Ticker) {
	defer callbackguard.LogPanic(context.Background(), "observer panicked")
	for range ticker.C {
		if callback := observer.Load(); callback != nil {
			(*callback)()
		}
	}
}

type Registry struct{ observer atomic.Pointer[func()] }

func (registry *Registry) SetObserver(callback func()) { registry.observer.Store(&callback) }

func (registry *Registry) Start() {
	go registry.fire()
	go registry.fireGuarded()
	go registry.fireThroughLoader()
	go registry.fireDeclaredFirst()
}

func (registry *Registry) fireThroughLoader() {
	load := registry.observer.Load
	if callback := load(); callback != nil {
		(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
	}
}

func (registry *Registry) fireDeclaredFirst() {
	var callback *func()
	callback = registry.observer.Load()
	if callback != nil {
		(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
	}
}

func (registry *Registry) fire() {
	callback := registry.observer.Load()
	if callback != nil {
		(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
	}
}

func (registry *Registry) fireGuarded() {
	defer callbackguard.LogPanic(context.Background(), "observer panicked")
	callback := registry.observer.Load()
	if callback != nil {
		(*callback)()
	}
}

func Synchronous() {
	if callback := observer.Load(); callback != nil {
		(*callback)()
	}
}

func InternalOnly(ticker *time.Ticker) {
	for range ticker.C {
		if callback := internal.Load(); callback != nil {
			(*callback)()
		}
		if cancel := cancelFn.Load(); cancel != nil {
			(*cancel)()
		}
	}
}

var swapped atomic.Pointer[func()]

func SetSwapped(callback func()) { swapped.Swap(&callback) }
func RunSwapped(ticker *time.Ticker) {
	for range ticker.C {
		if callback := swapped.Load(); callback != nil {
			(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
		}
	}
}
func RunSwappedGuarded(ticker *time.Ticker) {
	defer callbackguard.LogPanic(context.Background(), "observer panicked")
	for range ticker.C {
		if callback := swapped.Load(); callback != nil {
			(*callback)()
		}
	}
}
func RunValueSpec(ticker *time.Ticker) {
	for range ticker.C {
		var callback = observer.Load()
		if callback != nil {
			(*callback)() // want `recovercallback: \(\*callback\) is invoked with no recover in scope`
		}
	}
}
func RunValueSpecGuarded(ticker *time.Ticker) {
	defer callbackguard.LogPanic(context.Background(), "observer panicked")
	for range ticker.C {
		var callback = observer.Load()
		if callback != nil {
			(*callback)()
		}
	}
}
func recoverReturned() any { return recover() }
func RunReturnRecoverGuarded(ticker *time.Ticker) {
	defer recoverReturned()
	for range ticker.C {
		if callback := observer.Load(); callback != nil {
			(*callback)()
		}
	}
}
