// Package infinitescroll provides a sentinel-based infinite-scroll
// container that lazily fetches the next page of items via RPC as the
// user scrolls.
//
// The Go-side renders the first page (Items) plus a hidden sentinel
// element and a <noscript> "Load more" form. The runtime
// (core-ui/runtime/runtime.js) attaches an IntersectionObserver to the
// sentinel; when it intersects the viewport (rootMargin default
// "200px"), the runtime POSTs to the configured RPC path with the
// current cursor and appends the response HTML into the items
// container. An `X-Gofastr-Infinite-Cursor: <next>` response header
// drives the next call; an empty/missing header signals end-of-feed
// (the sentinel is removed and the observer disconnected).
//
// Accessibility:
//
//   - role="feed" with aria-label and aria-busy on the wrapper.
//   - aria-busy flips true → false across each fetch.
//   - The <noscript> "Load more" button gives non-JS users a
//     keyboard-operable fallback that submits to the same endpoint.
//
// Server contract (handlers writing the RPC response):
//
//   - Body: an HTML fragment (typically a list of items). Append-only.
//   - Header `X-Gofastr-Infinite-Cursor: <next-token>` for the next
//     call. Omit (or set empty) to indicate end-of-feed.
//
// See [Config] for the renderer surface; see [Render] for the entry
// point.
package infinitescroll
