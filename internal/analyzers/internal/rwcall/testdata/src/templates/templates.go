package templates

import (
	"bytes"
	htmltemplate "html/template"
	"net/http"
	texttemplate "text/template"
)

type htmlAlias = htmltemplate.Template
type textAlias = texttemplate.Template
type other struct{}

func (other) Execute(http.ResponseWriter, any) error { return nil }

func render(writer http.ResponseWriter, html htmltemplate.Template, text texttemplate.Template, aliasedHTML htmlAlias, aliasedText textAlias) {
	_ = html.Execute(writer, nil)                        // want `response write`
	_ = html.ExecuteTemplate(writer, "page", nil)        // want `response write`
	_ = text.Execute(writer, nil)                        // want `response write`
	_ = text.ExecuteTemplate(writer, "page", nil)        // want `response write`
	_ = aliasedHTML.Execute(writer, nil)                 // want `response write`
	_ = aliasedText.ExecuteTemplate(writer, "page", nil) // want `response write`
	_ = (&html).Execute(writer, nil)                     // want `response write`
	var buffer bytes.Buffer
	_ = html.Execute(&buffer, nil)
	_ = (other{}).Execute(writer, nil)
}
