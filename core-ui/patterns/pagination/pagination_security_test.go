package pagination

import (
	"net/url"
	"strings"
	"testing"
)

// TestCarryHrefPatternKeepsPageVerb pins that a query-string carry built
// from request-derived values (the patternWith / resource.Table
// construction: url.Values.Encode() + "&" + "p=%d") can never inject fmt
// directives into HrefPattern. The New() guard only requires a literal
// "%d" in the pattern, which survives, so nothing fails loudly — but
// fmt.Sprintf parses Encode()'s own %XX triples as flag/width/verb, the
// page argument gets consumed by the wrong verb, and every page link
// href comes out corrupted (sinks: pagination.go pageItem/prevNextItem).
func TestCarryHrefPatternKeepsPageVerb(t *testing.T) {
	for _, tc := range []struct{ name, search string }{
		{"amp", "a&b"},
		{"percent", "50% off"},
		{"utf8", "café"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := url.Values{"q": {tc.search}}.Encode()
			pattern := "?" + enc + "&p=%d"
			h := string(New(Config{Total: 3, Current: 1, HrefPattern: pattern}))
			if strings.Contains(h, "%!") {
				t.Errorf("SECURITY: [fmt-carry] search %q (carry %q) injected fmt directives into HrefPattern %q; page hrefs corrupted by fmt: %s", tc.search, enc, pattern, h[:min(len(h), 400)])
			}
			if !strings.Contains(h, "p=2") {
				t.Errorf("SECURITY: [fmt-carry] search %q: page-2 link must keep its p= param, got: %s", tc.search, h[:min(len(h), 400)])
			}
			if !strings.Contains(h, "q="+enc) {
				t.Errorf("SECURITY: [fmt-carry] search %q: carried value %q must round-trip untouched in page hrefs, got: %s", tc.search, enc, h[:min(len(h), 400)])
			}
		})
	}
}

// TestCarryHrefPatternIslandPushState pins the island-mode sinks: the same
// carried pattern is Sprintf'd into data-fui-push-state and the
// data-fui-rpc URL (pagination.go pageItemRPC/prevNextItemRPC/
// relativeQuery), so a hostile carry corrupts both the RPC path and the
// push-state URL, not just the <a> href.
func TestCarryHrefPatternIslandPushState(t *testing.T) {
	enc := url.Values{"q": {"a&b"}}.Encode()
	pattern := "?" + enc + "&p=%d"
	h := string(New(Config{Total: 3, Current: 1, HrefPattern: pattern,
		IslandSignal: "tbl", IslandEndpoint: "/orders"}))
	if strings.Contains(h, "%!") {
		t.Errorf("SECURITY: [fmt-carry] island RPC/push-state URLs corrupted by fmt directives in pattern %q: %s", pattern, h[:min(len(h), 400)])
	}
	if !strings.Contains(h, "p=2") {
		t.Errorf("SECURITY: [fmt-carry] island push-state must keep the p= param for page 2, got: %s", h[:min(len(h), 400)])
	}
}
