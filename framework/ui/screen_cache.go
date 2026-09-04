package ui

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Screen-cache invalidation: server-driven eviction of the runtime's
// per-tab SPA screen cache.
//
// The runtime keeps a small LRU of rendered screens so back/forward
// navigation is instant. After a mutation, cached copies of screens
// that render the mutated data are stale. A handler names them via the
// X-Gofastr-Invalidate response header (a JSON string array) and the
// runtime drops those entries when the fetch resolves 2xx, so the next
// visit re-fetches fresh state.
//
// Selector semantics (enforced client-side):
//
//   - "/orders":           drops /orders AND every cached query variant
//     (/orders?page=2, /orders?sort=asc, …). A mutation that stales a
//     list stales every filtered/paginated view of it.
//   - "/orders?page=2":     drops exactly that pathname+query entry.
//   - "*":                 clears the whole cache.
//
// No prefix/glob matching: "/orders" never matches "/orders/42".
//
// Scope: the cache is in-memory per tab, so the header only reaches the
// tab whose request carried it. Surfaces that must stay fresh across
// tabs belong on the polling rung (data-fui-poll), not the screen cache.
//
// Invalidation never re-renders the visible page. It only affects
// future navigations. Pair with data-fui-rpc-navigate (or
// __gofastr.refresh() client-side) when the mutation should also land
// the user on a freshly rendered screen.

// InvalidateScreens appends paths to the X-Gofastr-Invalidate response
// header. Call it from any handler reached via data-fui-rpc, a widget
// RPC, or SPA navigation; multiple calls accumulate into one JSON
// array, mirroring AddToast.
//
// Paths must be root-relative ("/orders", "/dashboard?range=7d") or the
// wildcard "*". Anything else: empty strings, absolute URLs,
// protocol-relative or bare-relative paths, or paths carrying control
// characters, is dropped silently: the values are only ever cache-map
// keys on the client, so an invalid one could never match an entry
// anyway. (Control bytes are rejected outright rather than escaped:
// DEL survives JSON encoding un-escaped, and an invalid header value
// is silently dropped by Go's HTTP/2 writer. A path that can't be a
// real cache key is not worth a malformed header.)
//
// The header is consumed on every 2xx mutation or navigation response
// the runtime dispatches: data-fui-rpc, widget RPC, SPA navigation,
// intercepted navigation, toggle/optimistic actions, and sortable-list
// reorders. Poll replies never consume it.
func InvalidateScreens(w http.ResponseWriter, paths ...string) {
	var valid []string
	for _, p := range paths {
		if p == "*" || (strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !hasCtl(p)) {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return
	}
	var list []string
	if existing := w.Header().Get("X-Gofastr-Invalidate"); existing != "" {
		// Malformed manual values are replaced, not preserved: a bad
		// prefix must not poison the whole header. The parse failure is
		// explicit so a half-decoded slice never marches on either.
		if err := json.Unmarshal([]byte(existing), &list); err != nil {
			list = nil
		}
	}
	list = append(list, valid...)
	enc, err := json.Marshal(list)
	if err != nil {
		return
	}
	w.Header().Set("X-Gofastr-Invalidate", string(enc))
}

// hasCtl reports whether s contains a C0 control byte or DEL.
func hasCtl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
