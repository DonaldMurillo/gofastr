// Package i18n is the stub of core/i18n the rawtext fixtures call.
package i18n

import (
	"context"
	"net/http"
)

type Phrase struct{ key, english string }

func Define(key, english string) Phrase { return Phrase{key, english} }

func (p Phrase) Text(ctx context.Context, params ...map[string]any) string { return p.english }

func (p Phrase) Code() string { return p.key }

type Translator struct{}

func (t *Translator) T(ctx context.Context, key string, params ...map[string]any) string { return key }

func Fill(s string, params map[string]any) string { return s }

func FillContext(ctx context.Context, s string, params map[string]any) string { return s }

// statusPhrases is package level inside a composite: quiet.
var statusPhrases = map[int]Phrase{
	404: Define("error.not_found", "not found"),
}

func HTTPError(w http.ResponseWriter, r *http.Request, status int, p Phrase) {
	//gofastr:allow(rawtext) the sanctioned writer
	http.Error(w, p.Text(r.Context()), status)
}

// Number writes v in the reader's locale.
func Number(ctx context.Context, v int) string { return "" }
