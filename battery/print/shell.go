package print

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// autoPrintScript is served verbatim from autoPrintPath. It is an
// EXTERNAL script (not inline) so it complies with the strict default
// CSP, which has no script-src 'unsafe-inline'. It opens the print
// dialog once the document has loaded.
const autoPrintScript = `addEventListener('load',function(){window.print();});` + "\n"

// effectivePage resolves a document's PageConfig against the battery
// default, filling any zero-valued fields so pageCSS always sees a
// complete, valid config.
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

// pageCSS renders the @page rule for a PageConfig. Every value flows
// through safeCSSLengthOr or a closed enum, so no caller-supplied string
// can inject CSS.
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

// shellInput carries everything renderShell needs to assemble a standalone
// print document.
type shellInput struct {
	Title        string      // <title>, escaped here
	Body         render.HTML // already-escaped component output
	PageCSS      string      // from pageCSS
	BaseCSS      string      // print base stylesheet
	ComponentCSS string      // scoped CSS for styled components used in Body
	DocCSS       string      // optional Document.Stylesheet
	AppCSSHref   string      // optional design-token stylesheet href; "" = none
	AutoPrintSrc string      // optional external autoprint script src; "" = none
}

// componentCSS collects the scoped stylesheets for every registered styled
// component referenced in the body. A print document has no runtime to
// lazy-load component CSS and doesn't run the uihost SSR collector, so
// without this a Build component built from framework/ui or
// core-ui/patterns (which ship their CSS as [data-fui-comp] scoped sheets)
// would render unstyled. Component CSS is var(--*)-based, so DefaultTheme
// is fine here — the concrete token values come from the linked app.css
// :root, not from these rules.
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
			b.WriteString(e.CSSFor(theme))
		}
	}
	return b.String()
}

// renderShell assembles the full <!doctype html> print document. It never
// links runtime.js — a print document is inert. The <style> blocks are
// server-generated/trusted; the only attacker-influenced value (Title) is
// escaped, and Body is already escaped by the component layer.
func renderShell(in shellInput) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString(`  <meta charset="utf-8">` + "\n")
	b.WriteString(`  <meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	fmt.Fprintf(&b, "  <title>%s</title>\n", render.Escape(in.Title))
	if in.AppCSSHref != "" {
		fmt.Fprintf(&b, "  <link rel=\"stylesheet\" href=\"%s\">\n", render.Escape(in.AppCSSHref))
	}
	fmt.Fprintf(&b, "  <style>%s</style>\n", in.BaseCSS)
	if strings.TrimSpace(in.ComponentCSS) != "" {
		fmt.Fprintf(&b, "  <style>%s</style>\n", in.ComponentCSS)
	}
	fmt.Fprintf(&b, "  <style>%s</style>\n", in.PageCSS)
	if strings.TrimSpace(in.DocCSS) != "" {
		fmt.Fprintf(&b, "  <style>%s</style>\n", in.DocCSS)
	}
	if in.AutoPrintSrc != "" {
		fmt.Fprintf(&b, "  <script src=\"%s\"></script>\n", render.Escape(in.AutoPrintSrc))
	}
	b.WriteString("</head>\n<body class=\"print-doc\">\n")
	b.WriteString(string(in.Body))
	b.WriteString("\n</body>\n</html>")
	return b.String()
}

// printBaseCSS is the built-in readable print stylesheet. It references
// design tokens via var(--*) with fallbacks so the document looks right
// whether or not the host's app.css is linked, strips shadows/backgrounds
// for ink economy under @media print, and ships page-break utilities.
const printBaseCSS = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body.print-doc {
  font-family: var(--font-sans, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif);
  font-size: 12pt;
  line-height: 1.5;
  color: var(--color-text, #111827);
  background: var(--color-background, #ffffff);
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}
body.print-doc h1, body.print-doc h2, body.print-doc h3 {
  line-height: 1.2;
  margin: 0 0 0.4em;
}
body.print-doc table { width: 100%; border-collapse: collapse; }
body.print-doc th, body.print-doc td {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border, #e5e7eb);
}
body.print-doc img { max-width: 100%; }
.page-break { break-after: page; page-break-after: always; }
.avoid-break { break-inside: avoid; page-break-inside: avoid; }
.print-only { display: block; }
.screen-only { display: none; }
@media screen {
  .print-only { display: none; }
  .screen-only { display: block; }
  body.print-doc { max-width: 820px; margin: 24px auto; padding: 0 16px; }
}
@media print {
  body.print-doc { max-width: none; margin: 0; padding: 0; }
  a[href]::after { content: ""; }
  * { box-shadow: none !important; }
}
`
