package ui

import (
	"context"
	stdhtml "html"
	"maps"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/markdown"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// MarkdownConfig configures a Markdown renderer.
type MarkdownConfig struct {
	// Source is the raw markdown text (required).
	Source string
	// Compact tightens spacing for inline previews.
	Compact bool
	// Measure caps the line length at --ui-markdown-measure (72ch).
	Measure bool
	ID      string
	Class   string
	// ExtraAttrs lands on the root <div>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the code blocks' words are said in.
	Ctx context.Context
}

// Markdown renders Source as themed prose, each fenced block a
// highlighted CodeBlock. It panics without Source.
func Markdown(cfg MarkdownConfig) render.HTML {
	if cfg.Source == "" {
		panic("ui: Markdown requires Source")
	}
	cls := "fui-markdown"
	if cfg.Compact {
		cls += " fui-markdown--compact"
	}
	if cfg.Measure {
		cls += " fui-markdown--measure"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs))
	body := enrichCodeBlocks(cfg.Ctx, scrollTables(string(markdown.RenderHTML(cfg.Source))))
	return markdownStyle.WrapHTML(render.Tag("div", attrs, render.HTML(body)))
}

// scrollTables wraps each table in a focusable box that scrolls sideways.
func scrollTables(body string) string {
	body = strings.ReplaceAll(body, "<table>", `<div class="fui-markdown__table" tabindex="0"><table>`)
	return strings.ReplaceAll(body, "</table>", "</table></div>")
}

// enrichCodeBlocks re-renders each escaped <pre><code> block core/markdown
// emits as a highlighted CodeBlock with a copy button.
func enrichCodeBlocks(ctx context.Context, body string) string {
	const openPre = `<pre tabindex="0"><code`
	const closer = `</code></pre>`
	var out strings.Builder
	i := 0
	for {
		idx := strings.Index(body[i:], openPre)
		if idx < 0 {
			out.WriteString(body[i:])
			break
		}
		idx += i
		out.WriteString(body[i:idx])
		rest := body[idx+len(openPre):]
		gt := strings.IndexByte(rest, '>')
		if gt < 0 {
			out.WriteString(body[idx:])
			break
		}
		end := strings.Index(rest[gt+1:], closer)
		if end < 0 {
			out.WriteString(body[idx:])
			break
		}
		lang := codeAttr(rest[:gt], `class="language-`)
		metaAttr := codeAttr(rest[:gt], `data-meta="`)
		meta := parseFenceMeta(metaAttr)
		raw := stdhtml.UnescapeString(rest[gt+1 : gt+1+end])
		extra := html.Attrs{}
		if metaAttr != "" {
			extra["data-meta"] = stdhtml.UnescapeString(metaAttr)
		}
		block := CodeBlock(CodeBlockConfig{
			Lines:          HighlightLines(raw, lang),
			Language:       lang,
			Filename:       meta.filename,
			LineNumbers:    meta.lineNumbers,
			Scroll:         meta.scroll,
			HighlightLines: meta.highlight,
			Diff:           meta.diff,
			HighlightWords: meta.words,
			Wrap:           meta.wrap,
			ShowCopy:       true,
			ExtraAttrs:     extra,
			Ctx:            ctx,
		})
		out.WriteString(string(block))
		i = idx + len(openPre) + gt + 1 + end + len(closer)
	}
	return out.String()
}

// codeAttr returns the quoted value after marker in a <code> tag's
// attributes, or "".
func codeAttr(attrs, marker string) string {
	_, after, ok := strings.Cut(attrs, marker)
	if !ok {
		return ""
	}
	if before, _, ok := strings.Cut(after, "\""); ok {
		return before
	}
	return ""
}

// fenceMeta is the CodeBlock options a fence's info string carries:
//
//	```go title="main.go" showLineNumbers {2,5-7} diff words="err,nil" wrap scroll
type fenceMeta struct {
	filename    string
	lineNumbers bool
	scroll      bool
	diff        bool
	wrap        bool
	highlight   []LineRange
	words       []string
}

func parseFenceMeta(meta string) fenceMeta {
	var out fenceMeta
	for _, tok := range splitFenceMeta(stdhtml.UnescapeString(meta)) {
		if len(tok) > 1 && tok[0] == '{' && tok[len(tok)-1] == '}' {
			if r, err := ParseLineRanges(tok[1 : len(tok)-1]); err == nil {
				out.highlight = r
			}
			continue
		}
		key, val, hasVal := strings.Cut(tok, "=")
		switch strings.ToLower(key) {
		case "title", "filename":
			if hasVal {
				out.filename = strings.Trim(val, `"'`)
			}
		case "showlinenumbers":
			out.lineNumbers = !hasVal || strings.Trim(val, `"'`) != "false"
		case "scroll":
			out.scroll = !hasVal || strings.Trim(val, `"'`) != "false"
		case "diff":
			out.diff = !hasVal || strings.Trim(val, `"'`) != "false"
		case "wrap":
			out.wrap = !hasVal || strings.Trim(val, `"'`) != "false"
		case "nowrap":
			out.wrap = false
		case "highlight":
			if hasVal {
				if r, err := ParseLineRanges(strings.Trim(val, `"'`)); err == nil {
					out.highlight = r
				}
			}
		case "words":
			if hasVal {
				for _, w := range strings.Split(strings.Trim(val, `"'`), ",") {
					if w != "" {
						out.words = append(out.words, w)
					}
				}
			}
		}
	}
	return out
}

// splitFenceMeta splits on whitespace outside quotes.
func splitFenceMeta(meta string) []string {
	var toks []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(meta); i++ {
		c := meta[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return toks
}

var markdownStyle = registry.RegisterStyle("ui-markdown", markdownCSS)

func markdownCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-markdown"] {
  display: block;
  color: var(--color-text, #09090B);
  font-size: var(--text-base, 1rem);
  line-height: calc(var(--leading-relaxed, 1.6) + 0.1);
  text-wrap: pretty;
}

/* Flow rhythm — comfortable paragraph spacing, no leading gap. */
[data-cui-comp="ui-markdown"] > * { margin-block: 0; }
[data-cui-comp="ui-markdown"] > * + * { margin-block-start: 1.15em; }
[data-cui-comp="ui-markdown"] > :first-child { margin-block-start: 0; }

/* Headings — generous space ABOVE (section separation, scaled by level),
   tight space BELOW (the heading binds to the text it introduces). */
[data-cui-comp="ui-markdown"] h1,
[data-cui-comp="ui-markdown"] h2,
[data-cui-comp="ui-markdown"] h3,
[data-cui-comp="ui-markdown"] h4 {
  color: var(--color-text, #09090B);
  line-height: var(--leading-tight, 1.2);
  text-wrap: balance;
}
[data-cui-comp="ui-markdown"] h1 { font-size: var(--text-3xl, 1.875rem);  font-weight: var(--font-weight-bold); letter-spacing: var(--tracking-snug, -0.01em); margin-block: 0 0.5em; }
[data-cui-comp="ui-markdown"] h2 { font-size: var(--text-2xl, 1.5rem); font-weight: var(--font-weight-bold); letter-spacing: var(--tracking-snug, -0.01em);  margin-block: 2.6em 0.55em; }
[data-cui-comp="ui-markdown"] h3 { font-size: var(--text-lg, 1.125rem); font-weight: calc(var(--font-weight-semibold, 600) + 50); margin-block: 1.9em 0.45em; }
[data-cui-comp="ui-markdown"] h4 { font-size: var(--text-base, 1rem);    font-weight: calc(var(--font-weight-semibold, 600) + 50); margin-block: 1.5em 0.35em; }
/* A heading straight after another heading shouldn't double the gap. */
[data-cui-comp="ui-markdown"] h2 + h3,
[data-cui-comp="ui-markdown"] h3 + h4 { margin-block-start: 0.9em; }
[data-cui-comp="ui-markdown"] > :first-child:is(h1,h2,h3,h4) { margin-block-start: 0; }

[data-cui-comp="ui-markdown"] a {
  color: var(--color-primary, #18181B);
  text-decoration: underline;
  text-underline-offset: 0.18em;
  text-decoration-thickness: from-font;
}
[data-cui-comp="ui-markdown"] strong { font-weight: calc(var(--font-weight-semibold, 600) + 50); color: var(--color-text, #09090B); }

/* Lists — hang the marker, give items room, tighten nested levels. */
[data-cui-comp="ui-markdown"] ul,
[data-cui-comp="ui-markdown"] ol { padding-inline-start: 1.5em; }
[data-cui-comp="ui-markdown"] li { margin-block: 0; }
[data-cui-comp="ui-markdown"] li + li { margin-block-start: 0.4em; }
[data-cui-comp="ui-markdown"] li > ul,
[data-cui-comp="ui-markdown"] li > ol { margin-block-start: 0.4em; }
[data-cui-comp="ui-markdown"] li::marker { color: var(--color-text-subtle, #71717A); }

/* Inline code — a quiet chip, not a button. */
[data-cui-comp="ui-markdown"] code {
  font-family: var(--font-mono, ui-monospace, monospace);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  padding: 0.12em 0.4em;
  border-radius: var(--radii-sm, 6px);
  font-size: 0.875em;
}

/* Fenced code blocks are upgraded to framed ui.CodeBlock surfaces (syntax
   highlighting + copy button) by enrichCodeBlocks — they bring their own
   chrome; we only give them a little extra room above. */
:where([data-cui-comp="ui-markdown"]) > [data-cui-comp="ui-code-block"] {
  margin-block-start: 1.5em;
}
/* Any RAW <pre> that slipped through unframed (no class) still reads well. */
[data-cui-comp="ui-markdown"] pre:not([class]) {
  margin-block-start: 1.5em;
  padding: var(--spacing-lg, 1rem) calc(var(--spacing-sm, 4px) * 4.4);
  background: var(--color-code-surface, #18181B);
  color: var(--color-code-text, #E4E4E7);
  border: var(--stroke-thin, 1px) solid var(--color-code-border, var(--color-border, #E4E4E7));
  border-radius: var(--radii-md, 8px);
  overflow-x: auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-relaxed, 1.6);
  tab-size: 2;
}
[data-cui-comp="ui-markdown"] pre:not([class]) code {
  background: transparent;
  color: inherit;
  padding: 0;
  font-size: inherit;
}

/* Blockquote — tinted panel with a full hairline border. Deliberately NOT a
   thick left accent stripe (that pattern reads as templated). */
[data-cui-comp="ui-markdown"] blockquote {
  padding: 0.85em 1.1em;
  background: var(--color-surface-soft, #F4F4F5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-markdown"] blockquote > :first-child { margin-block-start: 0; }

[data-cui-comp="ui-markdown"] hr {
  border: 0;
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  margin-block: 2.75em;
}

/* Tables — quiet header, roomy cells, hairline row rules. Each table sits
   in a scroll box (scrollTables), so a wide one scrolls sideways on a
   phone instead of running past, or being clipped by, the column. */
[data-cui-comp="ui-markdown"] .fui-markdown__table {
  overflow-x: auto;
  margin-block-start: 1.5em;
  container-type: inline-size;
}
[data-cui-comp="ui-markdown"] .fui-markdown__table:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle, #71717A);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-markdown"] table {
  border-collapse: collapse;
  width: 100%;
  font-size: 0.95em;
}
[data-cui-comp="ui-markdown"] th,
[data-cui-comp="ui-markdown"] td {
  padding: 0.5em 0.85em;
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  text-align: start;
  vertical-align: top;
  /* No column grows past 70% of the box the table scrolls in (a size
     container, so cqi tracks the column at any page width, not the
     window): past that its text wraps, and a run with no break point
     (code chips joined by slashes) breaks where it would overflow
     rather than widening the column. */
  max-inline-size: 70cqi;
  overflow-wrap: break-word;
}
[data-cui-comp="ui-markdown"] thead th {
  font-weight: calc(var(--font-weight-semibold, 600) + 50);
  color: var(--color-text, #09090B);
  border-block-end-width: var(--stroke-thick, 2px);
}
[data-cui-comp="ui-markdown"] tbody tr:last-child td { border-block-end: 0; }

[data-cui-comp="ui-markdown"] img { max-width: 100%; height: auto; border-radius: var(--radii-md, 8px); }

/* Compact variant — tighter rhythm for inline previews. */
.fui-markdown.fui-markdown--compact { line-height: var(--leading-relaxed, 1.6); }
:where(.fui-markdown).fui-markdown--measure { max-inline-size: var(--ui-markdown-measure, 72ch); }
.fui-markdown.fui-markdown--compact > * + * { margin-block-start: 0.7em; }
.fui-markdown.fui-markdown--compact h2 { font-size: var(--text-lg, 1.125rem); margin-block: 1.4em 0.4em; }
.fui-markdown.fui-markdown--compact h3 { font-size: var(--text-base, 1rem); margin-block: 1.1em 0.35em; }
.fui-markdown.fui-markdown--compact h4 { font-size: var(--text-base, 1rem); margin-block: 0.9em 0.3em; }
.fui-markdown.fui-markdown--compact pre { margin-block-start: 0.9em; }`
}
