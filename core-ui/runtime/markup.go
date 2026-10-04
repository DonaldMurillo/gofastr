package runtime

// The markup generation handshake. A runtime reads one attribute
// vocabulary (generation 2 is the data-cui-* spelling; generation 1,
// through v0.86, read data-fui-*), and a tab opened before a deploy
// keeps its runtime after the server moved on. Every navigation fetch
// the runtime makes names its kernel's generation in MarkupHeader
// (frag/kernel.js `_markup`); a host whose markup is a different
// generation answers with a body that reloads the document, because
// swapping in markup the live kernel cannot read leaves every
// interactive marker dead (a data-cui-rpc form submits as a native GET).
const (
	// MarkupHeader is the request header a navigation fetch carries.
	MarkupHeader = "X-Gofastr-Markup"
	// MarkupVersion is the generation this runtime reads and the host
	// renders. Bump it, here and in frag/kernel.js, when a change to
	// the attribute vocabulary leaves an older kernel unable to read
	// the new markup.
	MarkupVersion = "2"
)
