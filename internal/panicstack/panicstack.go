// Package panicstack cuts a stack taken while a panic unwinds down to
// the frames that say where it happened.
package panicstack

import "strings"

// Origin returns stack from the function that panicked on. The frames
// above it are the same in every panic: the goroutine's header,
// debug.Stack, the deferred function that took the stack, and the
// runtime's own panic call. A stack with no panic in it, or with nothing
// after the panic, comes back whole.
func Origin(stack string) string {
	// The last "panic(" frame is the one nearest the code that panicked;
	// a frame is two lines, the call and where it is.
	i := strings.LastIndex(stack, "\npanic(")
	if i < 0 {
		return stack
	}
	rest := stack[i+1:]
	for range 2 {
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			return stack
		}
		rest = rest[nl+1:]
	}
	if rest == "" {
		return stack
	}
	return rest
}
