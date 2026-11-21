// Direct imports beyond the standard library include golang.org/x/net
// and other GoFastr packages. The optional Bridge helpers use
// framework/event to fan out internal Emit and EmitAsync calls to
// See framework/docs/content/webhooks.md for the wiring guide.
package webhook
