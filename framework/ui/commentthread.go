package ui

// ─── CommentThread ──────────────────────────────────────────────────
//
// A discussion: comments with their replies nested under them, as deep
// as the conversation goes. Each comment is an <article> (a byline, then
// the body) in a list item, and its replies are a list inside that item,
// so a screen reader hears the depth as list nesting. Each level is
// indented a step and ruled on its leading edge so the eye can follow a
// reply back to its parent; in a narrow container the step shrinks, so
// a deep thread keeps a readable column on a phone. The body is the
// caller's markup (paragraphs, links); long unbroken words and URLs wrap
// instead of widening the page. Server-rendered, no script.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Comment is one comment and its replies.
type Comment struct {
	// ID is the comment's element id, so a link can point at it.
	// Optional.
	ID string
	// Byline is the line above the body: the author, the age, a link
	// to the comment. The caller's markup.
	Byline render.HTML
	// Body is the comment's text: the caller's markup, already safe
	// (escaped text, paragraphs, links).
	Body render.HTML
	// Replies are drawn nested under the comment, in order.
	Replies []Comment
}

// CommentThreadConfig configures a CommentThread.
type CommentThreadConfig struct {
	// Comments are the top-level comments, in order.
	Comments []Comment
	// Label names the thread for assistive tech ("Comments").
	// Required.
	Label string
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root list. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// style, aria-label and data-cui-*.
	ExtraAttrs html.Attrs
}

// CommentThread renders the thread. It panics without a Label.
func CommentThread(cfg CommentThreadConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: CommentThread requires Label — name the thread for assistive tech")
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-comment-thread"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	attrs["aria-label"] = cfg.Label
	// list-style: none drops list semantics in WebKit/VoiceOver; the
	// role keeps the nesting a screen reader announces as depth.
	attrs["role"] = "list"
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	items, _ := commentItems(cfg.Comments)
	return commentThreadStyle.WrapHTML(render.Tag("ul", attrs, items...))
}

// commentItems draws one level of the thread, recursing into replies.
// held reports whether any item holds caller markup: a subtree holding
// none is the component's own, marked internal at its top.
func commentItems(cs []Comment) (out []render.HTML, held bool) {
	out = make([]render.HTML, len(cs))
	for i, c := range cs {
		var parts []render.HTML
		if c.Byline != "" {
			parts = append(parts, render.Tag("div", map[string]string{"class": "fui-comment-thread__byline"}, c.Byline))
		}
		if c.Body != "" {
			parts = append(parts, render.Tag("div", map[string]string{"class": "fui-comment-thread__body"}, c.Body))
		}
		art := map[string]string{"class": "fui-comment-thread__comment"}
		if c.ID != "" {
			art["id"] = c.ID
		}
		article := render.Tag("article", art, parts...)
		if len(parts) == 0 {
			article = headless.Own(article)
		}
		kids := []render.HTML{article}
		repliesHeld := false
		if len(c.Replies) > 0 {
			var replies []render.HTML
			replies, repliesHeld = commentItems(c.Replies)
			list := render.Tag("ul", map[string]string{"class": "fui-comment-thread__replies", "role": "list"}, replies...)
			if !repliesHeld {
				list = headless.Own(list)
			}
			kids = append(kids, list)
		}
		item := render.Tag("li", map[string]string{"class": "fui-comment-thread__item"}, kids...)
		if len(parts) == 0 && !repliesHeld {
			item = headless.Own(item)
		} else {
			held = true
		}
		out[i] = item
	}
	return out, held
}

var commentThreadStyle = registry.RegisterStyle("ui-comment-thread", commentThreadCSS)

// Knobs: --ui-comment-thread-gap (the space between comments),
// --ui-comment-thread-indent (each reply level's step) and
// --ui-comment-thread-indent-compact (the step in a container narrower
// than 30rem).
func commentThreadCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-comment-thread"] {
  container-type: inline-size;
  display: grid;
  gap: var(--ui-comment-thread-gap, var(--spacing-lg, 16px));
  margin: 0;
  padding: 0;
  list-style: none;
  min-width: 0;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__item {
  min-width: 0;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__comment {
  display: grid;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__byline {
  color: var(--color-text-muted);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-snug, 1.4);
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__byline a {
  color: inherit;
  text-decoration: none;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__byline a:hover {
  color: var(--color-text);
  text-decoration: underline;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__body {
  color: var(--color-text);
  line-height: var(--leading-normal, 1.5);
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__body p {
  margin: 0;
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__body p + p {
  margin-block-start: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__body a {
  color: var(--color-primary);
}
[data-cui-comp="ui-comment-thread"] .fui-comment-thread__replies {
  display: grid;
  gap: var(--ui-comment-thread-gap, var(--spacing-lg, 16px));
  margin: 0;
  margin-block-start: var(--ui-comment-thread-gap, var(--spacing-lg, 16px));
  padding: 0;
  padding-inline-start: var(--ui-comment-thread-indent, var(--spacing-lg, 16px));
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border);
  list-style: none;
  min-width: 0;
}
@container (max-width: 30rem) {
  [data-cui-comp="ui-comment-thread"] .fui-comment-thread__replies {
    padding-inline-start: var(--ui-comment-thread-indent-compact, var(--spacing-sm, 4px));
  }
}
`
}
