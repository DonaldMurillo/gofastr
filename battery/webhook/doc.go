// Package webhook is the outbound-webhook battery for GoFastr.
//
// Apps publish events; subscribers are HTTP endpoints that receive
// signed POST requests. Failed deliveries retry with exponential
// backoff and are parked in a dead-letter list once the attempt budget
// is exhausted.
//
// The core package (Manager, Store, Sign/Verify) is dependency-free
// beyond the standard library. The optional Bridge helpers in
// bridge.go pull in framework/event so internal Emit/EmitAsync calls
// can auto-fan out to subscribers; if you don't use the bridge, that
// dependency is dead code.
//
// See docs/webhooks.md for the wiring guide.
package webhook
