// Package winproc holds the Win32 call shape: (*syscall.LazyProc).Call
// and (*syscall.Proc).Call return r1, r2 and an error that is ALWAYS
// non-nil by design (it is built from GetLastError, which is whatever
// the last failing call left behind). The API's result is r1, so `r, _,
// _ := proc.Call(...)` discards no refusal. TestDiscardedErrWin32Call
// runs this package with GOOS=windows: syscall.LazyProc exists only
// there.
package winproc

import "syscall"

var user32 = syscall.NewLazyDLL("user32.dll")

func messageBox() uintptr {
	r, _, _ := user32.NewProc("MessageBoxW").Call(0, 0, 0, 0)
	return r
}

func procCall(p *syscall.Proc) uintptr {
	r, _, _ := p.Call()
	return r
}

type lister struct{}

func (lister) List() ([]string, error) { return nil, nil }

// A method with the same result shape on another type still fires.
func other(l lister) int {
	names, _ := l.List() // want `assignment discards the error from List\(\)`
	return len(names)
}

// A Call method on a type that is not the Win32 proc still fires.
type rpc struct{}

func (rpc) Call(args ...uintptr) (uintptr, uintptr, error) { return 0, 0, nil }

func notWin32(c rpc) uintptr {
	r, _, _ := c.Call() // want `assignment discards the error from Call\(\)`
	return r
}
