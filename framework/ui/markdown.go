package ui

import (
	stdhtml "html"
	"maps"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/markdown"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Markdown ───────────────────────────────────────────────────────
//
// Themed wrapper over core/markdown. The core renderer produces
// semantic HTML; this component wraps that in a styled prose container
// so headings, lists, links, code blocks, and tables get theme-token
// colors + readable typography without the caller writing CSS.

// MarkdownConfig configures a Markdown renderer.
type MarkdownConfig struct {
	// Source is the raw markdown text (required).
	Source string
	// Compact tightens spacing, useful for inline previews where
	// hero-page paragraph rhythm would feel wrong.
	Compact bool
	ID      string
	Class   string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the prose container's
	// root <div>. Keys the component owns are dropped: class and id
	// (use Class / ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// Markdown renders the given Markdown source as themed HTML.
func Markdown(cfg MarkdownConfig) render.HTML {
	if cfg.Source == "" {
		panic("ui: Markdown requires Source")
	}
	cls := "ui-markdown"
	if cfg.Compact {
		cls += " ui-markdown--compact"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs))
	body := enrichCodeBlocks(string(markdown.RenderHTML(cfg.Source)))
	return markdownStyle.WrapHTML(render.Tag("div", attrs, render.HTML(body)))
}

// enrichCodeBlocks upgrades the plain `<pre><code class="language-X">…</code></pre>`
// blocks that core/markdown emits into framed ui.CodeBlock surfaces: syntax
// highlighting (via highlightLines) plus a copy button and chrome, the same
// component the rest of the framework uses for code. The fenced body arrives
// HTML-escaped (core/markdown escapes it), so `</code></pre>` is an
// unambiguous terminator; we un-escape back to source, tokenize, and re-emit.
func enrichCodeBlocks(body string) string {
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
		// The raw info string rides on the block root in data-meta:
		// recognized options are consumed below, everything else stays
		// addressable for tooling instead of vanishing with the <code>
		// tag it arrived on.
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
		})
		out.WriteString(string(block))
		i = idx + len(openPre) + gt + 1 + end + len(closer)
	}
	return out.String()
}

// codeAttr extracts one value from a `<code>` tag's attribute string, given
// everything up to and including its opening quote: ` class="language-go"`
// with marker `class="language-` → "go". Returns "" when absent.
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

// fenceMeta is what this renderer understands of a fence's options, which
// core/markdown parses off the info string and passes through verbatim in
// data-meta. All of them map onto CodeBlock:
//
//	```go title="main.go" showLineNumbers {2,5-7} diff words="err,nil" wrap scroll
//
// title (quoted or bare) names the block's chrome header; showLineNumbers turns
// on the gutter; scroll caps the body height; {1,3-5} (or highlight=1,3-5)
// highlights lines; diff marks +/- lines as added/removed (the LANGUAGE diff
// implies nothing, only the option does); words=a,b marks literal matches;
// wrap soft-wraps long lines (nowrap / wrap=false turn it off). Unknown
// tokens are ignored rather than rejected, so a fence carrying options for
// some other tool still renders; an invalid highlight or words spec is
// dropped the same way instead of failing the page, and a valid earlier
// value survives it (an invalid later value has nothing to win with).
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
		// Bare brace form: {1,3-5} is the convention several doc tools
		// use for line highlight; a synonym of highlight=1,3-5.
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
			// Bare flag, or an explicit showLineNumbers=false to turn it off.
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

// splitFenceMeta splits on whitespace, except inside quotes: a title with a
// space in it is one token.
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
	// Reading typography for long-form docs. The rhythm is em-based so it
	// scales with the host's body size, and HIERARCHICAL rather than a flat
	// gap between every element: paragraphs breathe, section headings open a
	// larger gap above and bind tightly to the content below, lists and code
	// get their own measure. Leading is 1.7: light-on-dark text reads lighter
	// and wants extra line-height (the canonical site theme is dark-first).
	return `[data-fui-comp="ui-markdown"] {
  display: block;
  color: var(--color-text, #18181B);
  font-size: var(--text-base, 1rem);
  line-height: 1.7;
  text-wrap: pretty;
}

/* Flow rhythm — comfortable paragraph spacing, no leading gap. */
[data-fui-comp="ui-markdown"] > * { margin-block: 0; }
[data-fui-comp="ui-markdown"] > * + * { margin-block-start: 1.15em; }
[data-fui-comp="ui-markdown"] > :first-child { margin-block-start: 0; }

/* Headings — generous space ABOVE (section separation, scaled by level),
   tight space BELOW (the heading binds to the text it introduces). */
[data-fui-comp="ui-markdown"] h1,
[data-fui-comp="ui-markdown"] h2,
[data-fui-comp="ui-markdown"] h3,
[data-fui-comp="ui-markdown"] h4 {
  color: var(--color-text, #18181B);
  line-height: 1.25;
  text-wrap: balance;
}
[data-fui-comp="ui-markdown"] h1 { font-size: var(--text-3xl, 1.875rem);  font-weight: 700; letter-spacing: -0.014em; margin-block: 0 0.5em; }
[data-fui-comp="ui-markdown"] h2 { font-size: var(--text-2xl, 1.5rem); font-weight: 700; letter-spacing: -0.01em;  margin-block: 2.6em 0.55em; }
[data-fui-comp="ui-markdown"] h3 { font-size: var(--text-lg, 1.125rem); font-weight: 650; margin-block: 1.9em 0.45em; }
[data-fui-comp="ui-markdown"] h4 { font-size: var(--text-base, 1rem);    font-weight: 650; margin-block: 1.5em 0.35em; }
/* A heading straight after another heading shouldn't double the gap. */
[data-fui-comp="ui-markdown"] h2 + h3,
[data-fui-comp="ui-markdown"] h3 + h4 { margin-block-start: 0.9em; }
[data-fui-comp="ui-markdown"] > :first-child:is(h1,h2,h3,h4) { margin-block-start: 0; }

[data-fui-comp="ui-markdown"] a {
  color: var(--color-primary, #4F46E5);
  text-decoration: underline;
  text-underline-offset: 0.18em;
  text-decoration-thickness: from-font;
}
[data-fui-comp="ui-markdown"] strong { font-weight: 650; color: var(--color-text, #18181B); }

/* Lists — hang the marker, give items room, tighten nested levels. */
[data-fui-comp="ui-markdown"] ul,
[data-fui-comp="ui-markdown"] ol { padding-inline-start: 1.5em; }
[data-fui-comp="ui-markdown"] li { margin-block: 0; }
[data-fui-comp="ui-markdown"] li + li { margin-block-start: 0.4em; }
[data-fui-comp="ui-markdown"] li > ul,
[data-fui-comp="ui-markdown"] li > ol { margin-block-start: 0.4em; }
[data-fui-comp="ui-markdown"] li::marker { color: var(--color-text-subtle, #71717A); }

/* Inline code — a quiet chip, not a button. */
[data-fui-comp="ui-markdown"] code {
  font-family: var(--font-mono, ui-monospace, monospace);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  padding: 0.12em 0.4em;
  border-radius: var(--radii-sm, 4px);
  font-size: 0.875em;
}

/* Fenced code blocks are upgraded to framed ui.CodeBlock surfaces (syntax
   highlighting + copy button) by enrichCodeBlocks — they bring their own
   chrome; we only give them a little extra room above. */
[data-fui-comp="ui-markdown"] > [data-fui-comp="ui-code-block"] {
  margin-block-start: 1.5em;
}
/* Any RAW <pre> that slipped through unframed (no class) still reads well. */
[data-fui-comp="ui-markdown"] pre:not([class]) {
  margin-block-start: 1.5em;
  padding: var(--spacing-lg, 1rem) 1.1rem;
  background: var(--color-code-surface, #18181B);
  color: var(--color-code-text, #E4E4E7);
  border: 1px solid var(--color-code-border, var(--color-border, #E4E4E7));
  border-radius: var(--radii-md, 8px);
  overflow-x: auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.65;
  tab-size: 2;
}
[data-fui-comp="ui-markdown"] pre:not([class]) code {
  background: transparent;
  color: inherit;
  padding: 0;
  font-size: inherit;
}

/* Blockquote — tinted panel with a full hairline border. Deliberately NOT a
   thick left accent stripe (that pattern reads as templated). */
[data-fui-comp="ui-markdown"] blockquote {
  padding: 0.85em 1.1em;
  background: var(--color-surface-soft, #F4F4F5);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-markdown"] blockquote > :first-child { margin-block-start: 0; }

[data-fui-comp="ui-markdown"] hr {
  border: 0;
  border-block-start: 1px solid var(--color-border, #E4E4E7);
  margin-block: 2.75em;
}

/* Tables — quiet header, roomy cells, hairline row rules. */
[data-fui-comp="ui-markdown"] table {
  border-collapse: collapse;
  width: 100%;
  margin-block-start: 1.5em;
  font-size: 0.95em;
}
[data-fui-comp="ui-markdown"] th,
[data-fui-comp="ui-markdown"] td {
  padding: 0.5em 0.85em;
  border-block-end: 1px solid var(--color-border, #E4E4E7);
  text-align: start;
  vertical-align: top;
}
[data-fui-comp="ui-markdown"] thead th {
  font-weight: 650;
  color: var(--color-text, #18181B);
  border-block-end-width: 2px;
}
[data-fui-comp="ui-markdown"] tbody tr:last-child td { border-block-end: 0; }

[data-fui-comp="ui-markdown"] img { max-width: 100%; height: auto; border-radius: var(--radii-md, 8px); }

/* Compact variant — tighter rhythm for inline previews. */
.ui-markdown.ui-markdown--compact { line-height: 1.6; }
.ui-markdown.ui-markdown--compact > * + * { margin-block-start: 0.7em; }
.ui-markdown.ui-markdown--compact h2 { font-size: var(--text-lg, 1.125rem); margin-block: 1.4em 0.4em; }
.ui-markdown.ui-markdown--compact h3 { font-size: var(--text-base, 1rem); margin-block: 1.1em 0.35em; }
.ui-markdown.ui-markdown--compact h4 { font-size: var(--text-base, 1rem); margin-block: 0.9em 0.3em; }
.ui-markdown.ui-markdown--compact pre { margin-block-start: 0.9em; }`
}
