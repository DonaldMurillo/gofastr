// Package pagination renders a numeric pagination nav.
//
// The component is fully server-rendered as a <nav> with aria-label
// "Pagination". Each page is an <a> link, with aria-current="page"
// on the active page. Adjacent pages and the first/last are always
// shown; intermediate gaps are rendered as a <span> ellipsis.
//
// Hosts pass a HrefPattern (e.g. "/items?page=%d") used to build the
// link URL for each page number.
package pagination

import (
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// Config configures the pagination nav.
type Config struct {
	// Total is the total number of pages.
	Total int

	// Current is the active page (1-indexed).
	Current int

	// HrefPattern is a Sprintf pattern with one %d placeholder for the
	// page number. Required.
	HrefPattern string

	// Window is the number of pages to show on each side of Current.
	// Default: 1 → previous, current, next plus first/last with ellipses.
	Window int

	// Label overrides the default "Pagination" aria-label.
	Label string

	// PrevLabel and NextLabel customize the prev/next link text.
	// Defaults: "Previous" / "Next".
	PrevLabel string
	NextLabel string

	// OmitPrevNext suppresses the prev/next anchors entirely.
	OmitPrevNext bool

	// IslandSignal turns this pagination into an island update trigger.
	// When non-empty, every page link renders as a `<button
	// data-fui-rpc="<endpoint>?<page-key>=N" data-fui-rpc-method="GET"
	// data-fui-rpc-signal="<name>">N</button>` instead of an `<a href>`.
	// Click → runtime fires the RPC → response replaces the signal-bound
	// container. The server-side handler is responsible for returning
	// the new HTML and (recommended) an `X-Gofastr-Push-State` header
	// so the URL stays in sync.
	//
	// Pair with IslandEndpoint (the URL the buttons hit) for full island
	// behavior. When unset, pagination falls back to plain <a href> links.
	IslandSignal string

	// IslandEndpoint is the URL the page-change RPC fires at. The HrefPattern's
	// %d placeholder still encodes the page number — the runtime sends the
	// resulting URL as the RPC path. Required when IslandSignal is set.
	IslandEndpoint string

	ID    string
	Class string
}

// New renders the pagination nav.
//
// Required: Total > 0, Current in [1, Total], HrefPattern containing
// one %d placeholder.
func New(cfg Config) render.HTML {
	if cfg.Total <= 0 {
		panic("pagination: Total must be > 0")
	}
	if cfg.Current < 1 || cfg.Current > cfg.Total {
		panic(fmt.Sprintf("pagination: Current %d out of range [1, %d]", cfg.Current, cfg.Total))
	}
	if !strings.Contains(cfg.HrefPattern, "%d") {
		panic("pagination: HrefPattern must contain %d")
	}

	window := cfg.Window
	if window < 1 {
		window = 1
	}
	label := cfg.Label
	if label == "" {
		label = "Pagination"
	}
	prevLabel := cfg.PrevLabel
	nextLabel := cfg.NextLabel
	if prevLabel == "" {
		prevLabel = "Previous"
	}
	if nextLabel == "" {
		nextLabel = "Next"
	}
	if cfg.OmitPrevNext {
		prevLabel, nextLabel = "", ""
	}

	cls := "pagination"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	listAttrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		listAttrs["id"] = cfg.ID
	}

	pages := pageNumbers(cfg.Total, cfg.Current, window)
	items := make([]render.HTML, 0, len(pages)+2)

	island := cfg.IslandSignal != "" && cfg.IslandEndpoint != ""

	if prevLabel != "" {
		if island {
			items = append(items, prevNextItemRPC(cfg.IslandEndpoint, cfg.IslandSignal,
				prevLabel, "prev", cfg.HrefPattern, cfg.Current-1, cfg.Current > 1))
		} else {
			items = append(items, prevNextItem(prevLabel, "prev",
				cfg.HrefPattern, cfg.Current-1, cfg.Current > 1))
		}
	}
	for _, p := range pages {
		if p == 0 {
			items = append(items, render.Tag("li",
				map[string]string{"class": "pagination-gap"},
				render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text("…")),
			))
			continue
		}
		if island {
			items = append(items, pageItemRPC(cfg.IslandEndpoint, cfg.IslandSignal,
				cfg.HrefPattern, p, p == cfg.Current))
		} else {
			items = append(items, pageItem(cfg.HrefPattern, p, p == cfg.Current))
		}
	}
	if nextLabel != "" {
		if island {
			items = append(items, prevNextItemRPC(cfg.IslandEndpoint, cfg.IslandSignal,
				nextLabel, "next", cfg.HrefPattern, cfg.Current+1, cfg.Current < cfg.Total))
		} else {
			items = append(items, prevNextItem(nextLabel, "next",
				cfg.HrefPattern, cfg.Current+1, cfg.Current < cfg.Total))
		}
	}

	return render.Tag("nav", map[string]string{"aria-label": label},
		render.Tag("ol", listAttrs, items...),
	)
}

func pageItem(pattern string, page int, current bool) render.HTML {
	if current {
		return render.Tag("li", nil,
			render.Tag("a", map[string]string{
				"href":         fmt.Sprintf(pattern, page),
				"aria-current": "page",
			}, render.Text(fmt.Sprintf("%d", page))),
		)
	}
	return render.Tag("li", nil,
		render.Tag("a", map[string]string{"href": fmt.Sprintf(pattern, page)},
			render.Text(fmt.Sprintf("%d", page))),
	)
}

func prevNextItem(label, kind, pattern string, page int, enabled bool) render.HTML {
	cls := "pagination-" + kind
	if !enabled {
		return render.Tag("li", map[string]string{"class": cls + " is-disabled"},
			render.Tag("span", map[string]string{"aria-disabled": "true"}, render.Text(label)),
		)
	}
	return render.Tag("li", map[string]string{"class": cls},
		render.Tag("a", map[string]string{
			"href":       fmt.Sprintf(pattern, page),
			"rel":        kind,
			"aria-label": label,
		}, render.Text(label)),
	)
}

// pageItemRPC renders a page link as a data-fui-rpc button. Used when
// IslandSignal + IslandEndpoint are set on the Config — a click fires
// the RPC; the response replaces the signal-bound wrapper. See
// core-ui/ARCHITECTURE.md ("In-page state change" + "URL params are the
// source of truth").
func pageItemRPC(islandEndpoint, signal, hrefPattern string, page int, current bool) render.HTML {
	rpcURL := islandEndpoint + relativeQuery(hrefPattern, page)
	pushState := fmt.Sprintf(hrefPattern, page)
	attrs := map[string]string{
		"type":                  "button",
		"data-fui-rpc":          rpcURL,
		"data-fui-rpc-method":   "GET",
		"data-fui-rpc-signal":   signal,
		"data-fui-push-state":   pushState,
	}
	if current {
		attrs["aria-current"] = "page"
	}
	return render.Tag("li", nil,
		render.Tag("button", attrs, render.Text(fmt.Sprintf("%d", page))),
	)
}

// prevNextItemRPC mirrors prevNextItem in island mode.
func prevNextItemRPC(islandEndpoint, signal, label, kind, hrefPattern string, page int, enabled bool) render.HTML {
	cls := "pagination-" + kind
	if !enabled {
		return render.Tag("li", map[string]string{"class": cls + " is-disabled"},
			render.Tag("span", map[string]string{"aria-disabled": "true"}, render.Text(label)),
		)
	}
	rpcURL := islandEndpoint + relativeQuery(hrefPattern, page)
	pushState := fmt.Sprintf(hrefPattern, page)
	return render.Tag("li", map[string]string{"class": cls},
		render.Tag("button", map[string]string{
			"type":                "button",
			"data-fui-rpc":        rpcURL,
			"data-fui-rpc-method": "GET",
			"data-fui-rpc-signal": signal,
			"data-fui-push-state": pushState,
			"rel":                 kind,
			"aria-label":          label,
		}, render.Text(label)),
	)
}

// relativeQuery extracts the `?…` part of an href pattern with the
// page number filled in. Used to compose the RPC URL from the
// IslandEndpoint + the existing href pattern. If the pattern has no
// `?`, returns "?p=<page>" as a fallback.
func relativeQuery(hrefPattern string, page int) string {
	rendered := fmt.Sprintf(hrefPattern, page)
	if i := strings.Index(rendered, "?"); i >= 0 {
		return rendered[i:]
	}
	return fmt.Sprintf("?p=%d", page)
}

// pageNumbers returns the page sequence to render. 0 marks an ellipsis.
// Always includes 1, total, and a window around current.
func pageNumbers(total, current, window int) []int {
	if total <= 7+2*(window-1) {
		out := make([]int, total)
		for i := 0; i < total; i++ {
			out[i] = i + 1
		}
		return out
	}
	left := current - window
	right := current + window
	if left < 2 {
		left = 2
	}
	if right > total-1 {
		right = total - 1
	}
	out := []int{1}
	if left > 2 {
		out = append(out, 0)
	}
	for i := left; i <= right; i++ {
		out = append(out, i)
	}
	if right < total-1 {
		out = append(out, 0)
	}
	out = append(out, total)
	return out
}

// BaseCSS returns the stylesheet for pagination. Tokens: --color-text,
// --color-primary, --color-border, --color-text-muted, --radii-md,
// --spacing-xs, --spacing-sm.
func BaseCSS() string {
	return `
.pagination {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 2px);
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: 0.9rem;
}
.pagination li {
  display: inline-flex;
}
/* Targets <a>, <button> (island mode), and <span> (current/disabled). */
.pagination a,
.pagination button,
.pagination span {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: 2.25rem;
  block-size: 2.25rem;
  padding: 0 var(--spacing-sm, 4px);
  border-radius: var(--radii-md, 8px);
  border: 1px solid transparent;
  background: transparent;
  text-decoration: none;
  color: var(--color-text, #1F2937);
  font: inherit;
  font-size: inherit;
  cursor: pointer;
}
.pagination span { cursor: default; }
.pagination a:hover,
.pagination button:hover {
  background: var(--color-surface, #FFFFFF);
  border-color: var(--color-border, #E5E7EB);
}
.pagination [aria-current="page"] {
  background: var(--color-primary, #4F46E5);
  color: white;
  font-weight: 600;
  border-color: var(--color-primary, #4F46E5);
}
.pagination .is-disabled span,
.pagination [aria-disabled="true"] {
  color: var(--color-text-muted, #6B7280);
  opacity: 0.5;
  cursor: not-allowed;
}
.pagination-gap span {
  border: 0;
  cursor: default;
}
`
}
