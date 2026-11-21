// Package webhook is the outbound-webhook battery for GoFastr.
//
// Apps publish events; subscribers are HTTP endpoints that receive
// signed POST requests. Failed deliveries retry with exponential
// backoff and are parked in a dead-letter list once the attempt budget
// is exhausted.
//
// Direct imports beyond the standard library include golang.org/x/net
// and other GoFastr packages. The optional Bridge helpers use
// framework/event to fan out internal Emit and EmitAsync calls to
// subscribers.
//
// See framework/docs/content/webhooks.md for the wiring guide.
package webhook
