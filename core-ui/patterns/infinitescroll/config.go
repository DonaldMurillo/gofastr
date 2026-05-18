package infinitescroll

import "github.com/DonaldMurillo/gofastr/core/render"

// Config configures an infinite-scroll wrapper. Items is the first
// SSR-rendered page; the runtime appends subsequent pages on scroll.
type Config struct {
	// ID is the wrapper id. Optional but recommended for testability.
	ID string

	// Class is added to the wrapper's class list.
	Class string

	// RPCPath is the URL the runtime POSTs to when the sentinel
	// intersects the viewport. The body carries `cursor=<token>` as
	// form-encoded data. Required.
	RPCPath string

	// Items is the first page of SSR-rendered items. Required.
	// Subsequent pages are appended into the items container at
	// runtime.
	Items []render.HTML

	// ItemsClass is added to the inner items container's class list.
	ItemsClass string

	// Cursor is the initial cursor token shipped to the runtime via
	// `data-fui-infinite-cursor`. The server's first RPC response
	// is the second page; the first page is the SSR Items above.
	Cursor string

	// RootMargin is the IntersectionObserver rootMargin used by the
	// runtime. Default "200px".
	RootMargin string

	// AriaLabel labels the role=feed wrapper. Default "Feed".
	AriaLabel string

	// LoadMoreLabel is the visible text on the <noscript> fallback
	// button. Default "Load more".
	LoadMoreLabel string
}
