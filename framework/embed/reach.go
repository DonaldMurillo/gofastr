package embed

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

var reservedPrefixes = []string{
	"/mcp",          // framework/app.go: every entity's MCP twin plus the control tools
	"/openapi.json", // core/openapi: the full route and schema inventory
	"/api/docs",     // core/openapi: the same, rendered
	"/api/llm.md",   // framework/crud: every entity, field, validator, relation
	"/.debug",       // framework/app.go: pid, goroutines, memory
	"/auth",         // battery/auth: token minting, 2FA, account linking
	"/admin",        // battery/admin: the back office, behind a wildcard policy
	"/print",        // battery/print: each request spawns headless Chrome
	"/semantic",     // battery/semantic: index/query over the whole corpus
	"/metrics",      // framework/app.go: route inventory and traffic shape
}

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
				"which is the default this exists to replace: list the prefixes the "+
				"surface actually posts to instead", surface, field, p)
	}
	for _, res := range reserved {
		if routesOverlap(cleaned, res) {
			return "", fmt.Errorf(
				"embed: surface %q: %s %q covers %q, which the framework mounts "+
					"itself: an embed grant must never reach it", surface, field, p, res)
		}
	}
	return cleaned, nil
}

// AddReservedPrefixes registers additional paths no surface may reach, and
// re-validates every declared surface against them. It errors naming the
// surface and prefix when one already covers a new prefix.
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
			if routesOverlap(s.path, reserved) {
				return fmt.Errorf(
					"embed: surface %q: Path %q covers %q, which a mounted battery serves "+
						": an embed grant must never reach it", name, s.path, reserved)
			}
			for _, rch := range s.Reach {
				if routesOverlap(rch, reserved) {
					return fmt.Errorf(
						"embed: surface %q: Reach entry %q covers %q, which a mounted battery "+
							"serves: an embed grant must never reach it", name, rch, reserved)
				}
			}
		}
	}
	h.reserved = append(h.reserved, added...)
	return nil
}

// RoutedPath returns the path the router will dispatch on, decoded one segment
// at a time, or ok=false when a segment decodes to a separator, a backslash,
// "." or "..", or does not decode at all.
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
	rest := p[len(prefix):]
	return strings.HasPrefix(rest, "/")
}

func routeWithin(pattern, p string) bool {
	if !isDynamicRoute(pattern) {
		return pathWithin(pattern, p)
	}
	if !strings.HasPrefix(pattern, "/") || !strings.HasPrefix(p, "/") {
		return false
	}
	want := strings.Split(pattern[1:], "/")
	got := strings.Split(p[1:], "/")
	for i, seg := range want {
		if i >= len(got) {
			return false
		}
		param, rest := routeSeg(seg)
		switch {
		case rest:
			return strings.Join(got[i:], "/") != ""
		case param:
			if !segAdmits(seg, got[i]) {
				return false
			}
		case seg != got[i]:
			return false
		}
	}
	return true
}

func routesOverlap(a, b string) bool {
	if !isDynamicRoute(a) && !isDynamicRoute(b) {
		return pathWithin(a, b) || pathWithin(b, a)
	}
	as := strings.Split(strings.TrimPrefix(a, "/"), "/")
	bs := strings.Split(strings.TrimPrefix(b, "/"), "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ap, ar := routeSeg(as[i])
		bp, br := routeSeg(bs[i])
		switch {
		case ar || br:
			return true
		case ap && bp:
			continue
		case ap:
			if !segAdmits(as[i], bs[i]) {
				return false
			}
		case bp:
			if !segAdmits(bs[i], as[i]) {
				return false
			}
		case as[i] != bs[i]:
			return false
		}
	}
	return true
}

var runtimeEndpoints = []string{
	"/__gofastr/runtime.js",
	"/__gofastr/color-scheme.js",
	"/__gofastr/actions.js",
	"/__gofastr/manifest.js",
	"/__gofastr/app.css",
	"/__gofastr/comp-bundle.css",
	"/__gofastr/sse",
	"/__gofastr/action",
	"/__gofastr/widgets",
	LoaderPath,
	RuntimePath,
	ExchangePath,
	RefreshPath,
}

var runtimePrefixes = []string{
	"/__gofastr/widget",
	"/__gofastr/comp",
	"/__gofastr/runtime",
	"/__gofastr/compute",
	"/__gofastr/pwa",
	"/__gofastr/icons",
	strings.TrimSuffix(SurfacePrefix, "/"),
}

func runtimeReach(p string) bool {
	if slices.Contains(runtimeEndpoints, p) {
		return true
	}
	for _, pre := range runtimePrefixes {
		if strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

var widgetLeaves = []string{"style.css", "state", "chrome"}

func (h *Host) widgetReach(surface *ResolvedSurface, p string) bool {
	if h.widgetOnSurface == nil {
		return false
	}
	rest, ok := strings.CutPrefix(p, "/core-ui/widget/")
	if !ok {
		return false
	}
	name, leaf, _ := strings.Cut(rest, "/")
	if name == "" || !slices.Contains(widgetLeaves, leaf) {
		return false
	}
	return h.widgetOnSurface(name, surface.path)
}

func reservedFor(reserved []string, p string) (string, bool) {
	for _, res := range reserved {
		if pathWithin(res, p) {
			return res, true
		}
	}
	return "", false
}

// MayReach reports whether a grant for this surface may be used on p: the
// surface's own route subtree, the runtime endpoints, or a declared Reach
// prefix. Pass the result of [RoutedPath].
func (s *ResolvedSurface) MayReach(p string) bool {
	if p == "" {
		return false
	}
	cleaned := path.Clean(p)
	if routeWithin(s.path, cleaned) {
		return true
	}
	if runtimeReach(cleaned) {
		return true
	}
	for _, r := range s.Reach {
		if pathWithin(r, cleaned) {
			return true
		}
	}
	return false
}
