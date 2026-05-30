// Package print is a GoFastr battery for printable documents. A host
// declares named, route-addressable print documents — the print-battery
// equivalent of a screen/route — and the battery server-renders each into
// a clean, chrome-free, print-friendly HTML page (its own @page size and
// margins, the browser's native print dialog, no nav/sidebar/runtime).
//
// The HTML core is pure Go with no extra dependencies. Real PDF output is
// opt-in through the pluggable PDFRenderer interface; the headless-Chromium
// implementation lives in the separate battery/print/chromepdf subpackage
// so this package never imports chromedp.
//
// See https://github.com/DonaldMurillo/gofastr for documentation.
package print
