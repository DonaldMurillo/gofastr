// Package sitesearch is full-text search over an app's rendered pages.
// browser client is three runtime modules: sitesearch, loaded by a
// [data-search] listbox such as ui.CommandPalette with Search set;
// results from the files with no server, the same way Search does; and
// (ui.SearchResults' [data-search-page]), updating results in place
// as the reader types.
package sitesearch
