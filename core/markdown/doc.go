// Package markdown is a small, dependency-free Markdown renderer.
//
// It parses a constrained subset of Markdown — enough for project docs and
// website content — and emits HTML directly. It is not CommonMark-strict;
// the goal is predictable output for the inputs we actually write, not
// edge-case fidelity.
//
// Supported block elements:
//   - ATX headings (#, ##, … up to ######)
//   - Paragraphs
//   - Fenced code blocks (``` with optional language tag)
//   - Unordered lists (-, *, +)
//   - Ordered lists (1., 2., …)
//   - Blockquotes (>)
//   - Horizontal rules (---, ***, ___)
//   - GFM-style tables (| col | col |)
//   - YAML-ish frontmatter (--- key: value --- at the top of the document)
//
// Supported inline elements:
//   - Bold (**…** or __…__)
//   - Italic (*…* or _…_)
//   - Inline code (`…`)
//   - Links ([text](url)) and images (![alt](url))
//
// HTML in source is escaped, never passed through. Code blocks and inline
// code are rendered without syntax highlighting; callers can post-process if
// they want highlighted output.
package markdown
