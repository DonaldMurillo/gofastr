// check-csp:ignore-file

package mcp

import (
	"fmt"
	"html/template"
	"strings"
)

const defaultWidgetRootID = "app"

// WidgetDocument is an MCP App widget's markup and code; HTML assembles it into the single-file document AppConfig.HTML wants, with the widget client inlined.
type WidgetDocument struct {
	// Title is the document title (escaped).
	Title string
	// Lang is the <html lang> value (escaped); default "en".
	Lang string
	// RootID is the id of the element wrapping Body (escaped); default "app".
	RootID string
	// Body is the author's markup, inserted verbatim.
	Body string
	// Script is the author's JavaScript, inserted verbatim after the widget client; it must not contain "</script" or "<!--".
	Script string
}

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

type widgetDocModel struct {
	Title  string
	Lang   string
	RootID string
	Body   template.HTML
	Client template.JS
	Script template.JS
}

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

// HTML returns the assembled document, or an error when Body and Script are both empty or Script holds "</script" or "<!--".
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
