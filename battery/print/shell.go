package print

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const autoPrintScript = `addEventListener('load',function(){window.print();});` + "\n"

func effectivePage(doc *PageConfig, def PageConfig) PageConfig {
	p := def
	if doc != nil {
		if doc.Size != "" {
			p.Size = doc.Size
		}
		if doc.Orientation != "" {
			p.Orientation = doc.Orientation
		}
		if doc.CustomWidth != "" {
			p.CustomWidth = doc.CustomWidth
		}
		if doc.CustomHeight != "" {
			p.CustomHeight = doc.CustomHeight
		}
		if doc.Margin.Top != "" {
			p.Margin.Top = doc.Margin.Top
		}
		if doc.Margin.Right != "" {
			p.Margin.Right = doc.Margin.Right
		}
		if doc.Margin.Bottom != "" {
			p.Margin.Bottom = doc.Margin.Bottom
		}
		if doc.Margin.Left != "" {
			p.Margin.Left = doc.Margin.Left
		}
	}
	return p
}

func pageCSS(p PageConfig) string {
	var size string
	switch p.Size {
	case Custom:
		size = safeCSSLengthOr(p.CustomWidth, "210mm") + " " + safeCSSLengthOr(p.CustomHeight, "297mm")
	case Letter, Legal, A4:
		size = string(p.Size)
		if p.Orientation == Landscape {
			size += " landscape"
		}
	default:
		size = "A4"
		if p.Orientation == Landscape {
			size += " landscape"
		}
	}
	margin := strings.Join([]string{
		safeCSSLengthOr(p.Margin.Top, "12mm"),
		safeCSSLengthOr(p.Margin.Right, "12mm"),
		safeCSSLengthOr(p.Margin.Bottom, "12mm"),
		safeCSSLengthOr(p.Margin.Left, "12mm"),
	}, " ")
	return fmt.Sprintf("@page { size: %s; margin: %s; }", size, margin)
}

type shellInput struct {
	Lang         string
	Title        string
	Body         render.HTML
	PageCSS      string
	BaseCSS      string
	ComponentCSS string
	DocCSS       string
	AppCSSHref   string
	AutoPrintSrc string
	CSP          string
}

func componentCSS(body render.HTML) string {
	names := registry.Scan(string(body))
	if len(names) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(names))
	uniq := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			uniq = append(uniq, n)
		}
	}
	sort.Strings(uniq)
	theme := style.DefaultTheme()
	var b strings.Builder
	for _, n := range uniq {
		if e, ok := registry.Lookup(n); ok {
			b.WriteString(e.ServedCSSFor(theme))
		}
	}
	return b.String()
}

func renderShell(in shellInput) string {
	appCSSHref := urlsafe.Clean(in.AppCSSHref, urlsafe.Resource)
	autoPrintSrc := urlsafe.Clean(in.AutoPrintSrc, urlsafe.Resource)
	lang := in.Lang
	if lang == "" {
		lang = "en"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html>\n<html dir=\"%s\" lang=\"%s\">\n<head>\n", i18n.Dir(lang), render.Escape(lang))
	b.WriteString(`  <meta charset="utf-8">` + "\n")
	b.WriteString(`  <meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	csp := in.CSP
	if csp == "" {
		csp = printCSP
	}
	fmt.Fprintf(&b, "  <meta http-equiv=\"Content-Security-Policy\" content=\"%s\">\n", render.Escape(csp))
	fmt.Fprintf(&b, "  <title>%s</title>\n", render.Escape(in.Title))
	if appCSSHref != "" {
		fmt.Fprintf(&b, "  <link rel=\"stylesheet\" href=\"%s\">\n", render.Escape(appCSSHref))
	}
	fmt.Fprintf(&b, "  <style>%s</style>\n", in.BaseCSS)
	if strings.TrimSpace(in.ComponentCSS) != "" {
		fmt.Fprintf(&b, "  <style>%s</style>\n", in.ComponentCSS)
	}
	fmt.Fprintf(&b, "  <style>%s</style>\n", in.PageCSS)
	if strings.TrimSpace(in.DocCSS) != "" {
		fmt.Fprintf(&b, "  <style>%s</style>\n", in.DocCSS)
	}
	if autoPrintSrc != "" {
		fmt.Fprintf(&b, "  <script src=\"%s\"></script>\n", render.Escape(autoPrintSrc))
	}
	b.WriteString("</head>\n<body class=\"print-doc\">\n")
	b.WriteString(string(in.Body))
	b.WriteString("\n</body>\n</html>")
	return b.String()
}

const printBaseCSS = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body.print-doc {
  font-family: var(--font-sans, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif);
  font-size: 12pt;
  line-height: var(--leading-normal, 1.5);
  color: var(--color-text, #09090B);
  background: var(--color-background, #ffffff);
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}
body.print-doc h1, body.print-doc h2, body.print-doc h3 {
  line-height: var(--leading-tight, 1.2);
  margin: 0 0 0.4em;
}
body.print-doc table { width: 100%; border-collapse: collapse; }
body.print-doc th, body.print-doc td {
  text-align: start;
  padding: calc(var(--spacing-sm, 4px) * 1.5) var(--spacing-md, 8px);
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
body.print-doc img { max-width: 100%; }
.page-break { break-after: page; page-break-after: always; }
.avoid-break { break-inside: avoid; page-break-inside: avoid; }
.print-only { display: block; }
.screen-only { display: none; }
@media screen {
  .print-only { display: none; }
  .screen-only { display: block; }
  body.print-doc { max-width: var(--ui-print-doc-max-width, 820px); margin: var(--spacing-xl, 24px) auto; padding: 0 var(--spacing-lg, 16px); }
}
@media print {
  body.print-doc { max-width: none; margin: 0; padding: 0; }
  a[href]::after { content: ""; }
  * { box-shadow: none !important; }
}
`
