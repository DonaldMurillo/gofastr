package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── DiffViewer ─────────────────────────────────────────────────────
//
// Line-by-line diff renderer for unified-format input (the same kind
// `diff -u` / `git diff` produces). Two modes:
//
//   - DiffUnified (default) — single column; +/- prefix per line
//   - DiffSplit             — two columns: removed on left, added on right
//
// Input is the raw diff body (no headers needed). The viewer parses
// it line by line.

// DiffMode picks the layout.
type DiffMode string

const (
	DiffUnified DiffMode = ""
	DiffSplit   DiffMode = "split"
)

// DiffViewerConfig configures a DiffViewer.
type DiffViewerConfig struct {
	// Patch is the raw unified-diff body (required). Lines prefixed
	// "+" are additions, "-" are removals, " " (space) are context,
	// "@@" lines are hunk headers, "---" / "+++" headers are
	// rendered as a filename row.
	Patch string
	// Mode picks unified (default) or split layout.
	Mode DiffMode
	// LeftLabel / RightLabel show above split columns (defaults
	// "Old" / "New").
	LeftLabel  string
	RightLabel string
	ID         string
	Class      string
}

// DiffViewer renders a unified-diff body as a styled view.
func DiffViewer(cfg DiffViewerConfig) render.HTML {
	if cfg.Patch == "" {
		panic("ui: DiffViewer requires Patch")
	}
	switch cfg.Mode {
	case DiffUnified, DiffSplit:
	default:
		panic("ui: DiffViewer unknown Mode " + string(cfg.Mode) +
			` — pick one of: "" (unified), split`)
	}

	cls := "ui-diff-viewer"
	if cfg.Mode == DiffSplit {
		cls += " ui-diff-viewer--split"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	lines := strings.Split(cfg.Patch, "\n")
	var sb strings.Builder

	if cfg.Mode == DiffSplit {
		left := cfg.LeftLabel
		if left == "" {
			left = "Old"
		}
		right := cfg.RightLabel
		if right == "" {
			right = "New"
		}
		sb.WriteString(`<div class="ui-diff-viewer__header"><div class="ui-diff-viewer__header-cell">`)
		sb.WriteString(escapeXML(left))
		sb.WriteString(`</div><div class="ui-diff-viewer__header-cell">`)
		sb.WriteString(escapeXML(right))
		sb.WriteString(`</div></div>`)

		// Walk hunks: emit removed in left, added in right; context
		// duplicated in both columns; align by zipping +/- runs.
		i := 0
		for i < len(lines) {
			ln := lines[i]
			if strings.HasPrefix(ln, "@@") {
				sb.WriteString(`<div class="ui-diff-viewer__hunk">`)
				sb.WriteString(escapeXML(ln))
				sb.WriteString(`</div>`)
				i++
				continue
			}
			if strings.HasPrefix(ln, "---") || strings.HasPrefix(ln, "+++") {
				sb.WriteString(`<div class="ui-diff-viewer__file">`)
				sb.WriteString(escapeXML(ln))
				sb.WriteString(`</div>`)
				i++
				continue
			}
			// Buffer a contiguous +/-/space run.
			var removed, added []string
			for i < len(lines) {
				cur := lines[i]
				if strings.HasPrefix(cur, "@@") || strings.HasPrefix(cur, "---") || strings.HasPrefix(cur, "+++") {
					break
				}
				if strings.HasPrefix(cur, "-") {
					removed = append(removed, cur[1:])
					i++
					continue
				}
				if strings.HasPrefix(cur, "+") {
					added = append(added, cur[1:])
					i++
					continue
				}
				// Context line — flush buffered, emit context in both cols.
				flushSplit(&sb, removed, added)
				removed, added = nil, nil
				ctx := cur
				if strings.HasPrefix(cur, " ") {
					ctx = cur[1:]
				}
				sb.WriteString(`<div class="ui-diff-viewer__row ui-diff-viewer__row--context">`)
				sb.WriteString(`<div class="ui-diff-viewer__cell"><pre class="ui-diff-viewer__code" tabindex="0">`)
				sb.WriteString(escapeXML(ctx))
				sb.WriteString(`</pre></div><div class="ui-diff-viewer__cell"><pre class="ui-diff-viewer__code" tabindex="0">`)
				sb.WriteString(escapeXML(ctx))
				sb.WriteString(`</pre></div></div>`)
				i++
			}
			flushSplit(&sb, removed, added)
		}
	} else {
		// Unified mode.
		for _, ln := range lines {
			switch {
			case strings.HasPrefix(ln, "@@"):
				sb.WriteString(`<div class="ui-diff-viewer__hunk">`)
				sb.WriteString(escapeXML(ln))
				sb.WriteString(`</div>`)
			case strings.HasPrefix(ln, "---") || strings.HasPrefix(ln, "+++"):
				sb.WriteString(`<div class="ui-diff-viewer__file">`)
				sb.WriteString(escapeXML(ln))
				sb.WriteString(`</div>`)
			case strings.HasPrefix(ln, "+"):
				sb.WriteString(`<div class="ui-diff-viewer__line ui-diff-viewer__line--add"><span class="ui-diff-viewer__gutter">+</span><pre class="ui-diff-viewer__code" tabindex="0">`)
				sb.WriteString(escapeXML(ln[1:]))
				sb.WriteString(`</pre></div>`)
			case strings.HasPrefix(ln, "-"):
				sb.WriteString(`<div class="ui-diff-viewer__line ui-diff-viewer__line--remove"><span class="ui-diff-viewer__gutter">−</span><pre class="ui-diff-viewer__code" tabindex="0">`)
				sb.WriteString(escapeXML(ln[1:]))
				sb.WriteString(`</pre></div>`)
			default:
				body := ln
				if strings.HasPrefix(ln, " ") {
					body = ln[1:]
				}
				sb.WriteString(`<div class="ui-diff-viewer__line ui-diff-viewer__line--context"><span class="ui-diff-viewer__gutter"> </span><pre class="ui-diff-viewer__code" tabindex="0">`)
				sb.WriteString(escapeXML(body))
				sb.WriteString(`</pre></div>`)
			}
		}
	}

	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return diffViewerStyle.WrapHTML(render.Tag("div", attrs, render.HTML(sb.String())))
}

func flushSplit(sb *strings.Builder, removed, added []string) {
	if len(removed) == 0 && len(added) == 0 {
		return
	}
	n := len(removed)
	if len(added) > n {
		n = len(added)
	}
	for i := 0; i < n; i++ {
		sb.WriteString(`<div class="ui-diff-viewer__row">`)
		// Left column: removed (or empty).
		sb.WriteString(`<div class="ui-diff-viewer__cell ui-diff-viewer__cell--remove">`)
		if i < len(removed) {
			sb.WriteString(`<pre class="ui-diff-viewer__code" tabindex="0">`)
			sb.WriteString(escapeXML(removed[i]))
			sb.WriteString(`</pre>`)
		}
		sb.WriteString(`</div>`)
		// Right column: added (or empty).
		sb.WriteString(`<div class="ui-diff-viewer__cell ui-diff-viewer__cell--add">`)
		if i < len(added) {
			sb.WriteString(`<pre class="ui-diff-viewer__code" tabindex="0">`)
			sb.WriteString(escapeXML(added[i]))
			sb.WriteString(`</pre>`)
		}
		sb.WriteString(`</div>`)
		sb.WriteString(`</div>`)
	}
}

// _ keeps strconv used (if we later add line-number gutters).
var _ = strconv.Itoa

var diffViewerStyle = registry.RegisterStyle("ui-diff-viewer", diffViewerCSS)

func diffViewerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-diff-viewer"] {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-sm, 0.85rem);
  line-height: 1.5;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  overflow: hidden;
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__hunk {
  padding: var(--spacing-sm, 4px) var(--spacing-md, 12px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-xs, 0.75rem);
  border-block: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__file {
  padding: var(--spacing-sm, 4px) var(--spacing-md, 12px);
  color: var(--color-text, #18181B);
  font-weight: 600;
  font-size: var(--text-sm, 0.85rem);
  border-block-end: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__line {
  display: grid;
  grid-template-columns: 2ch 1fr;
  gap: var(--spacing-sm, 8px);
  padding-inline: var(--spacing-md, 12px);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__gutter {
  user-select: none;
  text-align: end;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__code {
  margin: 0;
  padding: 0;
  font: inherit;
  white-space: pre;
  overflow-x: auto;
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__line--add {
  background: color-mix(in srgb, var(--color-success, #16A34A) 12%, transparent);
}
[data-fui-comp="ui-diff-viewer"] .ui-diff-viewer__line--remove {
  background: color-mix(in srgb, var(--color-danger, #DC2626) 12%, transparent);
}

/* Split layout */
.ui-diff-viewer--split .ui-diff-viewer__header,
.ui-diff-viewer--split .ui-diff-viewer__row {
  display: grid;
  grid-template-columns: 1fr 1fr;
}
.ui-diff-viewer--split .ui-diff-viewer__header-cell {
  padding: var(--spacing-sm, 4px) var(--spacing-md, 12px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
  border-block-end: 1px solid var(--color-border, #E4E4E7);
}
.ui-diff-viewer--split .ui-diff-viewer__header-cell + .ui-diff-viewer__header-cell {
  border-inline-start: 1px solid var(--color-border, #E4E4E7);
}
.ui-diff-viewer--split .ui-diff-viewer__cell {
  padding: 0 var(--spacing-md, 12px);
}
.ui-diff-viewer--split .ui-diff-viewer__cell + .ui-diff-viewer__cell {
  border-inline-start: 1px solid var(--color-border, #E4E4E7);
}
.ui-diff-viewer--split .ui-diff-viewer__cell--add {
  background: color-mix(in srgb, var(--color-success, #16A34A) 12%, transparent);
}
.ui-diff-viewer--split .ui-diff-viewer__cell--remove {
  background: color-mix(in srgb, var(--color-danger, #DC2626) 12%, transparent);
}`
}
