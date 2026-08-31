// check-csp:ignore-file
//
// This file emits ONE inline <script>, in widgetDocumentTmpl, and it was
// only ever passing the inline-script lint by accident: the checker
// classified the first <script> tag in a literal and stopped, so the
// preceding <script src="…"> masked it. See widgetDocumentTmpl's own
// comment for why the rule's premise does not describe this surface --
// an MCP App resource is a single self-contained HTML document delivered
// over resources/read and rendered by the chat host in its own sandboxed
// iframe, under a CSP the host assembles from AppConfig.CSP. There is no
// second URL on our origin for the host to fetch author code from.
package mcp

import (
	"fmt"
	"html/template"
	"strings"
)

// defaultWidgetRootID is the id WidgetDocument gives the root element that
// wraps the author's body markup when RootID is not set.
const defaultWidgetRootID = "app"

// WidgetDocument is the author-owned half of an MCP App widget document:
// the markup and the code. HTML assembles it into the single-file HTML
// document AppConfig.HTML wants, so the boilerplate an author would
// otherwise retype per widget — doctype, head, charset/viewport, the root
// element, and above all the `<script src>` that loads the widget client —
// is right by construction.
//
// The client script URL is not a field: it is [WidgetClientScriptURL], the
// same constant the server mounts the client route at. That is the drift
// this type exists to prevent — a one-character typo in a hand-written
// script URL renders a widget that silently never receives anything, with
// no error to grep for.
//
// The builder emits structure only: no CSS, no design tokens, no default
// styling of any kind. The chat host owns the widget's design language;
// the widget client applies the host's theme signals (HostContext.theme,
// HostContext.styles) to the document root at runtime, and author CSS
// references those signals instead of inventing a palette. See
// framework/docs/content/agent-host.md for the convention.
//
// Field trust matches AppConfig.HTML itself: Title, Lang, and RootID are
// data and are context-escaped by html/template (HTML text, attribute
// value); Body and Script are author-authored document content and pass
// through verbatim, exactly as a hand-written HTML string would.
type WidgetDocument struct {
	// Title is the document title (text-escaped).
	Title string
	// Lang is the <html lang> value (attribute-escaped). Default "en".
	Lang string
	// RootID is the id of the root element wrapping Body (attribute-
	// escaped). Default "app".
	RootID string
	// Body is the author's markup, inserted verbatim inside the root
	// element.
	Body string
	// Script is the author's JavaScript statements, inserted verbatim in a
	// classic inline <script> after the widget client loads, so it can use
	// window.__gofastrMcpApp immediately. It must not contain the sequence
	// "</script" (case-insensitive) or "<!--": the first terminates the
	// script element early and the second can put the HTML parser into a
	// state where the closing tag no longer closes. HTML rejects both with
	// an error rather than shipping a document the parser silently
	// truncates. Split string literals ("<\/script>", "<\!--") to avoid
	// the sequences.
	Script string
}

// widgetDocumentTmpl is the whole document. html/template's contextual
// analysis is load-bearing here: Title renders in an HTML-text context,
// Lang/RootID/ScriptURL in attribute contexts, and each is escaped for
// its own context — the three escapes a string concat would conflate.
// code, and this document is not served by the web host: an MCP App
// resource is a single self-contained HTML document delivered over
// resources/read and rendered by the chat host in its own sandboxed
// iframe, whose CSP the host assembles from AppConfig.CSP. There is no
// second URL on our origin for the host to fetch author code from, and
// AppConfig.HTML has always been documented as single-file HTML with
// inline JS/CSS. The rule's premise -- the web host's default-src 'self'
// policy with no unsafe-inline -- does not describe this surface.
var widgetDocumentTmpl = template.Must(template.New("widgetdoc").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
</head>
<body>
<div id="{{.RootID}}">{{.Body}}</div>
<script src="{{.ScriptURL}}"></script>
<script>
{{.Script}}
</script>
</body>
</html>
`))

// widgetDocModel is the executed template data. Body and Script are
// pre-typed as template.HTML / template.JS — the author-authored document
// content — while the data fields stay plain strings so html/template
// escapes them.
type widgetDocModel struct {
	Title     string
	Lang      string
	RootID    string
	ScriptURL string
	Body      template.HTML
	Script    template.JS
}

// HTML assembles the widget document and returns it for AppConfig.HTML.
//
// It errors when Body and Script are both empty (an empty widget is an
// authoring mistake, the same class RegisterApp guards) and when Script
// contains a sequence the HTML parser cannot carry inside an inline
// script element ("</script", case-insensitive, or "<!--").
func (d WidgetDocument) HTML() (string, error) {
	if strings.TrimSpace(d.Body) == "" && strings.TrimSpace(d.Script) == "" {
		return "", fmt.Errorf("mcp: widget document needs a Body, a Script, or both")
	}
	if i := strings.Index(strings.ToLower(d.Script), "</script"); i >= 0 {
		return "", fmt.Errorf("mcp: widget Script contains %q at offset %d: that sequence terminates the inline script element; write it as \"<\\/script>\" inside string literals",
			d.Script[i:i+len("</script")], i)
	}
	if strings.Contains(d.Script, "<!--") {
		return "", fmt.Errorf("mcp: widget Script contains \"<!--\": it can put the HTML parser into a state where the closing script tag no longer closes; write it as \"<\\!--\" inside string literals")
	}

	lang := d.Lang
	if lang == "" {
		lang = "en"
	}
	rootID := d.RootID
	if rootID == "" {
		rootID = defaultWidgetRootID
	}

	var b strings.Builder
	if err := widgetDocumentTmpl.Execute(&b, widgetDocModel{
		Title:     d.Title,
		Lang:      lang,
		RootID:    rootID,
		ScriptURL: WidgetClientScriptURL,
		Body:      template.HTML(d.Body),
		Script:    template.JS(d.Script),
	}); err != nil {
		return "", fmt.Errorf("mcp: widget document: %w", err)
	}
	return b.String(), nil
}
