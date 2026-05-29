package tree

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. CSS auto-loads on first
// appearance via the runtime's data-fui-comp scanner. Apps override
// the visual defaults via theme tokens.
var Style = registry.RegisterStyle("tree", styleFn)

// Render renders the tree. The first visible treeitem carries
// tabindex=0 so Tab from the page lands on the tree; all other
// treeitems get tabindex=-1 (roving tabindex per WAI-ARIA tree).
func Render(cfg Config) render.HTML {
	if cfg.ID == "" {
		panic("tree: Render requires ID")
	}
	if cfg.Label == "" {
		panic("tree: Render requires Label")
	}
	if len(cfg.Nodes) == 0 {
		panic("tree: Render requires at least one root Node")
	}
	// Pre-check: if any node (or descendant) uses LazyPath, SignalPrefix must be set.
	if needsSignals(cfg.Nodes) && cfg.SignalPrefix == "" {
		panic("tree: Render requires SignalPrefix when any node uses LazyPath")
	}

	cls := "tree"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	wrapAttrs := map[string]string{
		"id":         cfg.ID,
		"class":      cls,
		"role":       "tree",
		"aria-label": cfg.Label,
	}
	rendered := make([]render.HTML, len(cfg.Nodes))
	firstRendered := false
	for i, n := range cfg.Nodes {
		rendered[i] = renderNode(n, 1, i+1, len(cfg.Nodes), cfg.SignalPrefix, !firstRendered)
		firstRendered = true
	}
	return Style.WrapHTML(render.Tag("ul", wrapAttrs, rendered...))
}

func needsSignals(nodes []Node) bool {
	for _, n := range nodes {
		if n.LazyPath != "" {
			return true
		}
		if needsSignals(n.Children) {
			return true
		}
	}
	return false
}

func renderNode(n Node, level, pos, setSize int, signalPrefix string, isFirstFocusable bool) render.HTML {
	if n.ID == "" {
		panic("tree: Node requires ID")
	}
	if n.Label == "" {
		panic("tree: Node requires Label")
	}

	isBranch := len(n.Children) > 0 || n.LazyPath != ""

	liAttrs := map[string]string{
		"id":              n.ID,
		"role":            "treeitem",
		"aria-level":      strconv.Itoa(level),
		"aria-posinset":   strconv.Itoa(pos),
		"aria-setsize":    strconv.Itoa(setSize),
		"class":           "tree__item",
	}
	if isFirstFocusable {
		liAttrs["tabindex"] = "0"
	} else {
		liAttrs["tabindex"] = "-1"
	}
	if isBranch {
		if n.Expanded {
			liAttrs["aria-expanded"] = "true"
		} else {
			liAttrs["aria-expanded"] = "false"
		}
	}
	if n.Selected {
		liAttrs["aria-selected"] = "true"
	}

	// Row: toggle (branches only) + label (or link).
	rowChildren := []render.HTML{}
	if isBranch {
		signalName := signalPrefix + "-" + n.ID
		toggleAttrs := map[string]string{
			"type":                  "button",
			"class":                 "tree__toggle",
			"aria-hidden":           "true",
			"tabindex":              "-1",
			"data-fui-tree-toggle":  "",
		}
		if n.LazyPath != "" && len(n.Children) == 0 {
			// Lazy: clicking the toggle fires RPC; response populates
			// the child <ul role="group"> via signal swap.
			toggleAttrs["data-fui-rpc"] = n.LazyPath
			toggleAttrs["data-fui-rpc-method"] = "POST"
			toggleAttrs["data-fui-rpc-signal"] = signalName
		}
		rowChildren = append(rowChildren,
			render.Tag("button", toggleAttrs, render.Raw("▶")),
		)
	}
	// A dangerous Href (javascript:/vbscript:/data:/protocol-relative/
	// control bytes) is dropped and the node degrades to a plain label
	// rather than a clickable XSS vector.
	if href := safeURL(n.Href); href != "" {
		rowChildren = append(rowChildren, render.Tag("a", map[string]string{
			"href":  href,
			"class": "tree__label",
		}, render.Text(n.Label)))
	} else {
		rowChildren = append(rowChildren, render.Tag("span", map[string]string{
			"class": "tree__label",
		}, render.Text(n.Label)))
	}
	row := render.Tag("div", map[string]string{"class": "tree__row"}, rowChildren...)

	liBody := []render.HTML{row}
	if isBranch {
		// Render group container — populated up-front (Children) or
		// awaiting RPC swap (LazyPath).
		groupAttrs := map[string]string{
			"role":  "group",
			"class": "tree__group",
		}
		if !n.Expanded {
			groupAttrs["hidden"] = ""
		}
		if n.LazyPath != "" && len(n.Children) == 0 {
			signalName := signalPrefix + "-" + n.ID
			groupAttrs["data-fui-signal"] = signalName
			groupAttrs["data-fui-signal-mode"] = "html"
		}
		childRendered := make([]render.HTML, len(n.Children))
		for i, c := range n.Children {
			childRendered[i] = renderNode(c, level+1, i+1, len(n.Children), signalPrefix, false)
		}
		liBody = append(liBody, render.Tag("ul", groupAttrs, childRendered...))
	}

	return render.Tag("li", liAttrs, liBody...)
}

// safeURL returns u if it is safe to render as an href, and "" if it
// carries a script-executing or origin-ambiguous scheme. Permitted:
// http(s), mailto, tel, relative paths, fragment- and query-only
// references. Dropped: javascript:/vbscript:/data:/file:/blob: and any
// other non-allow-listed scheme, protocol-relative "//host", and any
// value containing control bytes or percent-encoded CR/LF. Mirrors
// framework/ui/safety.go::safeURL — the patterns builders bypass that
// layer, so the allow-list is enforced here.
func safeURL(u string) string {
	if u == "" {
		return ""
	}
	for i := 0; i < len(u); i++ {
		if c := u[i]; c < 0x20 || c == 0x7f {
			return ""
		}
	}
	trimmed := strings.TrimLeft(u, " \t")
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return ""
	}
	// Protocol-relative URLs are ambiguous about origin trust.
	if strings.HasPrefix(trimmed, "//") {
		return ""
	}
	// Fragment-only, query-only, or relative paths pass.
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		return u
	}
	for i := 0; i < len(trimmed); i++ {
		switch c := trimmed[i]; c {
		case ':':
			switch strings.ToLower(trimmed[:i]) {
			case "http", "https", "mailto", "tel":
				return u
			default:
				return ""
			}
		case '/', '?', '#':
			// No scheme before the first path/query/fragment delimiter
			// — relative reference, allowed.
			return u
		}
	}
	// No colon — bare relative reference.
	return u
}
