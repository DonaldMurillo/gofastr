// Package sitesearch is full-text search over an app's rendered pages.
//
// The battery renders every page a static export would write (dynamic
// routes through StaticPaths), reads each page's <main> into a title and
// h2/h3 sections, and builds an inverted index. On a live server it
// answers GET /__gofastr/search/query; in a static export it writes the
// same index as content-hashed JSON files under /__gofastr/search/. The
// browser client is two runtime modules: sitesearch, loaded by a
// [data-search] listbox such as ui.CommandPalette with Search set, and
// sitesearch-static, which loads only on an exported page and ranks
// results from the files with no server, the same way Search does.
//
// Content opts out with data-search-ignore, or a page with a robots
// noindex meta. Guide: gofastr docs site-search.
package sitesearch
