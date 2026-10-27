// check-csp:ignore-file
//
// This file emits two inline <script> elements, in widgetDocumentTmpl:
// the widget client and the author's code. See widgetDocumentTmpl's own
// comment for why the inline-script rule's premise does not describe
// this surface -- an MCP App resource is a single self-contained HTML
// document delivered over resources/read and rendered by the chat host
// in its own sandboxed iframe, under a CSP the host assembles from
// AppConfig.CSP. There is no URL on our origin the frame can fetch code
// from: it resolves every relative URL against the host.
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
// element, and above all the widget client — is right by construction.
//
// The widget client ([WidgetClientJS]) is inlined, not linked. The chat
// host renders the document in a sandboxed srcdoc frame on the HOST's
// origin, so a `<script src>` naming a path on this server resolves
// against the host instead, 404s there, and the widget silently never
// receives anything. Inlining keeps the document single-file, as
// AppConfig.HTML requires, and needs no script-src origin in AppConfig.CSP.
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
	// classic inline <script> after the widget client runs, so it can use
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
// Lang/RootID in attribute contexts, and each is escaped for its own
// context — the escapes a string concat would conflate. The two scripts
// are inline because this document is not served by the web host: an MCP
// App resource is a single self-contained HTML document delivered over
// resources/read and rendered by the chat host in its own sandboxed
// iframe, whose CSP the host assembles from AppConfig.CSP. There is no
// URL on our origin the frame can fetch code from, and AppConfig.HTML has
// always been documented as single-file HTML with inline JS/CSS. The
// inline-script rule's premise -- the web host's default-src 'self'
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
<script>{{.Client}}</script>
<script>
{{.Script}}
</script>
</body>
</html>
`))

// widgetDocModel is the executed template data. Body, Client and Script
// are pre-typed as template.HTML / template.JS — document content, each
// checked by inlineScriptHazard where it lands in a script — while the
// data fields stay plain strings so html/template escapes them.
type widgetDocModel struct {
	Title  string
	Lang   string
	RootID string
	Body   template.HTML
	Client template.JS
	Script template.JS
}

// inlineScriptHazard names the first sequence in js that an inline
// <script> element cannot carry: "</script" (ASCII case-insensitive)
// closes the element early, and "<!--" can put the parser into a state
// where the closing tag no longer closes. It returns "" when js is safe.
// The scan folds ASCII only, so the offset it reports indexes js itself.
func inlineScriptHazard(js string) (seq string, at int) {
	for i := range len(js) {
		if js[i] != '<' {
			continue
		}
		if rest := js[i:]; strings.HasPrefix(rest, "<!--") {
			return "<!--", i
		} else if len(rest) >= len("</script") && strings.EqualFold(rest[:len("</script")], "</script") {
			return rest[:len("</script")], i
		}
	}
	return "", -1
}

// HTML assembles the widget document and returns it for AppConfig.HTML.
//
// It errors when Body and Script are both empty (an empty widget is an
// authoring mistake, the same class RegisterApp guards) and when Script
// contains a sequence the HTML parser cannot carry inside an inline
// script element ("</script", case-insensitive, or "<!--"). The widget
// client is held to the same rule, so a client that ever carried one
// fails here instead of shipping cut short in every widget.
func (d WidgetDocument) HTML() (string, error) {
	if strings.TrimSpace(d.Body) == "" && strings.TrimSpace(d.Script) == "" {
		return "", fmt.Errorf("mcp: widget document needs a Body, a Script, or both")
	}
	switch seq, at := inlineScriptHazard(d.Script); seq {
	case "":
	case "<!--":
		return "", fmt.Errorf("mcp: widget Script contains \"<!--\" at offset %d: it can put the HTML parser into a state where the closing script tag no longer closes; write it as \"<\\!--\" inside string literals", at)
	default:
		return "", fmt.Errorf("mcp: widget Script contains %q at offset %d: that sequence terminates the inline script element; write it as \"<\\/script>\" inside string literals", seq, at)
	}
	client := string(widgetClientJSBytes)
	if seq, at := inlineScriptHazard(client); seq != "" {
		return "", fmt.Errorf("mcp: the widget client contains %q at offset %d and cannot be inlined", seq, at)
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
		Title:  d.Title,
		Lang:   lang,
		RootID: rootID,
		Body:   template.HTML(d.Body),
		Client: template.JS(client),
		Script: template.JS(d.Script),
	}); err != nil {
		return "", fmt.Errorf("mcp: widget document: %w", err)
	}
	return b.String(), nil
}
