package panicstack

import "testing"

func TestOrigin(t *testing.T) {
	const top = "goroutine 7 [running]:\nruntime/debug.Stack()\n\t/go/debug/stack.go:26 +0x5e\nmw.func1()\n\t/mw.go:9 +0x1\n"
	const origin = "app.handler(...)\n\t/app/h.go:12 +0x2\nnet/http.HandlerFunc.ServeHTTP(...)\n\t/go/http/server.go:1 +0x3\n"
	const call = "panic({0x1, 0x2})\n\t/go/runtime/panic.go:860 +0x13a\n"
	for name, c := range map[string]struct{ in, want string }{
		"a panic's stack": {top + call + origin, origin},
		// The frames of a panic raised while another unwinds: the
		// nearest one to the code is the last.
		"a panic within a panic": {top + "panic({0x1})\n\t/p.go:1\nmw.again()\n\t/mw.go:20\n" + call + origin, origin},
		// Nothing to cut to: the stack comes back whole.
		"no panic frame":            {top + origin, top + origin},
		"nothing after the panic":   {top + call, top + call},
		"the panic frame cut short": {top + "panic({0x1, 0x2})\n\t/go/runtime/panic.go:860", top + "panic({0x1, 0x2})\n\t/go/runtime/panic.go:860"},
		"empty":                     {"", ""},
		// A function whose name ends in "panic(" is not the runtime's.
		"a function named like it": {"goroutine 1 [running]:\napp.dontpanic(...)\n\t/app/a.go:1\napp.caller()\n\t/app/b.go:2\n", "goroutine 1 [running]:\napp.dontpanic(...)\n\t/app/a.go:1\napp.caller()\n\t/app/b.go:2\n"},
	} {
		if got := Origin(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
	}
}
