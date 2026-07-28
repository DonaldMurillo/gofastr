package embed

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// reservedPrefixes are paths a Surface.Reach may never cover.
//
// Every one is mounted by the framework or one of its batteries rather than by
// the app author, which is exactly why an allow-list alone is not enough: an
// author who cannot get an embed working will reach for a wider prefix, and the
// prefixes that matter most are the ones they did not write and may not know
// exist. Refusing at boot is the same move New already makes for
// GrantMaxAge <= GrantTTL — a configuration that cannot be right should not
// start.
//
// This list is deliberately about FRAMEWORK-mounted routes. An app's own
// privileged routes are the app's to keep out of Reach; there is no way for
// this package to know them.
//
// The DEFAULT prefixes are listed here; several batteries let an app relocate
// them (admin.Config.PathPrefix, print.Config.PathPrefix, the auth plugin's
// WithPrefix), and an app that moves one would lose the protection entirely.
// AddReservedPrefixes closes that: the framework registers each battery's
// ACTUAL configured prefix at mount, so relocating a battery moves its
// protection with it.
var reservedPrefixes = []string{
	"/mcp",          // framework/app.go — every entity's MCP twin plus the control tools
	"/openapi.json", // core/openapi — the full route and schema inventory
	"/api/docs",     // core/openapi — the same, rendered
	"/api/llm.md",   // framework/crud — every entity, field, validator, relation
	"/.debug",       // framework/app.go — pid, goroutines, memory
	"/auth",         // battery/auth — token minting, 2FA, account linking
	"/admin",        // battery/admin — the back office, behind a wildcard policy
	"/print",        // battery/print — each request spawns headless Chrome
	"/semantic",     // battery/semantic — index/query over the whole corpus
	"/metrics",      // framework/app.go — route inventory and traffic shape
}

// normalizeReach cleans and validates one Path or Reach entry against reserved,
// the host's effective reserved-prefix list.
func normalizeReach(surface, field, p string, reserved []string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("embed: surface %q: empty %s entry", surface, field)
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("embed: surface %q: %s %q must start with /", surface, field, p)
	}
	cleaned := path.Clean(p)
	if cleaned == "/" {
		return "", fmt.Errorf(
			"embed: surface %q: %s %q would let an embed reach the whole app, "+
				"which is the default this exists to replace — list the prefixes the "+
				"surface actually posts to instead", surface, field, p)
	}
	for _, res := range reserved {
		if pathWithin(res, cleaned) || pathWithin(cleaned, res) {
			return "", fmt.Errorf(
				"embed: surface %q: %s %q covers %q, which the framework mounts "+
					"itself — an embed grant must never reach it", surface, field, p, res)
		}
	}
	return cleaned, nil
}

// AddReservedPrefixes registers additional paths no surface may reach, and
// re-validates every surface already declared against them.
//
// The framework calls this at mount time with each privileged battery's ACTUAL
// configured prefix, because the built-in list can only name defaults: an app
// that sets admin.Config.PathPrefix = "/back-office" would otherwise keep the
// protection on "/admin", which it no longer uses, and lose it on the prefix it
// does.
//
// Re-validation is the point. Surfaces are declared before mount, so a Path or
// Reach that only becomes reserved once a battery is mounted has to be caught
// here or not at all. The error names both the surface and the prefix.
func (h *Host) AddReservedPrefixes(prefixes ...string) error {
	added := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p == "" || !strings.HasPrefix(p, "/") {
			continue
		}
		cleaned := path.Clean(p)
		if cleaned == "/" {
			continue
		}
		if !slices.Contains(h.reserved, cleaned) && !slices.Contains(added, cleaned) {
			added = append(added, cleaned)
		}
	}
	if len(added) == 0 {
		return nil
	}
	for _, name := range h.names {
		s := h.surfaces[name]
		for _, reserved := range added {
			if pathWithin(reserved, s.path) || pathWithin(s.path, reserved) {
				return fmt.Errorf(
					"embed: surface %q: Path %q covers %q, which a mounted battery serves "+
						"— an embed grant must never reach it", name, s.path, reserved)
			}
			for _, rch := range s.Reach {
				if pathWithin(reserved, rch) || pathWithin(rch, reserved) {
					return fmt.Errorf(
						"embed: surface %q: Reach entry %q covers %q, which a mounted battery "+
							"serves — an embed grant must never reach it", name, rch, reserved)
				}
			}
		}
	}
	h.reserved = append(h.reserved, added...)
	return nil
}

// RoutedPath returns the path the ROUTER will dispatch on — decoded one segment
// at a time — or ok=false when the request must be refused before any
// authorization decision is made.
//
// It exists because the gate and the router disagreed about what "the path" is,
// and the disagreement was exploitable in both directions.
//
// MayReach used to decide on r.URL.Path, which is fully percent-decoded, and
// then cleaned it. net/http's ServeMux matches patterns against
// r.URL.EscapedPath(), where an encoded separator or an encoded dot segment is
// an ordinary byte sequence sitting INSIDE one segment. So a request for
//
//	GET /api/docs/%2e%2e/%2e%2e/reports
//
// decoded to "/api/docs/../../reports", which path.Clean collapsed to
// "/reports" — inside the surface's own subtree, so the gate admitted it. The
// router collapsed nothing, matched the subtree pattern "/api/docs/", and ran a
// handler that reservedPrefixes exists to keep grants away from. The mirror
// image, "/__gofastr%2Fprivate", read as a runtime endpoint to the gate and as
// a single opaque segment to the router, reaching an app's own "/{slug}".
//
// Cleaning cannot fix this. Normalising ONE of the two strings is precisely
// what opens the gap, so a stricter clean would only move it. The only correct
// move is to refuse any path whose segments do not survive decoding intact, and
// then compare on the same segments the router will see.
//
// Nothing legitimate is lost: a path segment never needs to contain an encoded
// "/" or to spell "." or "..". Ordinary escapes (%20, %2B, a UTF-8 name) decode
// to themselves and pass.
func RoutedPath(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	esc := u.EscapedPath()
	if esc == "" || esc[0] != '/' {
		return "", false
	}
	segs := strings.Split(esc, "/")
	out := make([]string, len(segs))
	for i, seg := range segs {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return "", false
		}
		// An encoded separator or dot segment is the whole attack: it is one
		// segment to the router and several to anything that decodes first.
		// Backslash joins it because some proxies normalise it to "/" before
		// the app ever sees the request.
		if strings.ContainsAny(dec, `/\`) {
			return "", false
		}
		if dec == "." || dec == ".." {
			return "", false
		}
		out[i] = dec
	}
	return strings.Join(out, "/"), true
}

// pathWithin reports whether p is prefix, or sits beneath it on a segment
// boundary. It is the whole matching rule, so "/api/orders" admits
// "/api/orders/42" and refuses "/api/orders-archive".
func pathWithin(prefix, p string) bool {
	if prefix == "" || p == "" {
		return false
	}
	if p == prefix {
		return true
	}
	if !strings.HasPrefix(p, prefix) {
		return false
	}
	// Boundary: the next byte after the prefix must start a new segment.
	rest := p[len(prefix):]
	return strings.HasPrefix(rest, "/")
}

// MayReach reports whether a grant for this surface may be used on p.
//
// Three things are in reach: the surface's own Path subtree, the runtime's
// /__gofastr/* endpoints (which are already grant-aware and scoped per surface
// — the widget catalog substitutes the grant's own surface path rather than
// trusting the caller), and each declared Reach prefix.
//
// Pass the result of [RoutedPath], never a raw r.URL.Path. The cleaning below
// is a backstop for direct callers, and cleaning alone is NOT sufficient: it
// collapses dot segments that the router does not, which is the bug RoutedPath
// exists to close. It stays because for a caller that skips RoutedPath,
// cleaning fails closed on "/reports/../admin/users" while not cleaning admits
// it under the "/reports" prefix.
func (s *ResolvedSurface) MayReach(p string) bool {
	if p == "" {
		return false
	}
	cleaned := path.Clean(p)
	if pathWithin(s.path, cleaned) {
		return true
	}
	if pathWithin("/__gofastr", cleaned) {
		return true
	}
	for _, r := range s.Reach {
		if pathWithin(r, cleaned) {
			return true
		}
	}
	return false
}
