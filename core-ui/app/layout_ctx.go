package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

const docShellSwapPrefix = "doc:"

type docShell struct {
	lang      string
	skipLabel string
	vtKinds   string
}

type docShellKey struct{}

func withDocShell(ctx context.Context, lang, skipLabel, vtKinds string) context.Context {
	return context.WithValue(ctx, docShellKey{}, docShell{lang: lang, skipLabel: skipLabel, vtKinds: vtKinds})
}

func docShellAttrs(ctx context.Context) html.Attrs {
	ds, ok := ctx.Value(docShellKey{}).(docShell)
	if !ok {
		return nil
	}
	attrs := html.Attrs{
		"data-cui-lang":       ds.lang,
		"data-cui-dir":        i18n.Dir(ds.lang),
		"data-cui-skip-label": ds.skipLabel,
	}
	if ds.vtKinds != "" {
		attrs["data-cui-vt-kinds"] = ds.vtKinds
	}
	return attrs
}
