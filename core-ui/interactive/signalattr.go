package interactive

import "strings"

// signalAttrAllowList is the set of attribute names a signal may drive.
//
// Allow-list, not deny-list. A deny-list has to enumerate every
// attribute that executes, `srcdoc` (a whole document), `style` (CSS),
// `data-behavior` (the runtime's <script src> sink), `sandbox` (removes
// an iframe's restrictions), every `on*`, and every privileged
// `data-cui-*`, and it loses the moment a new one is added. The
// allow-list only has to name what bindings legitimately write.
//
// The URL-valued members are additionally scheme-checked, at SSR by
// sanitizeSignalURL and on every client-side update by the runtime's
// _isUnsafeSignalUrl.
//
// disabled, checked, hidden, open and selected are HTML boolean
// attributes: on whenever present, whatever the value. The runtime's
// attr mode (core-ui/runtime/frag/signals.js) names the same five and
// removes them for a false value instead of writing "false", and so does
// the server render (core-ui/store's booleanSignalAttrs); a boolean
// attribute added here must join both lists.
var signalAttrAllowList = map[string]bool{
	"value": true, "href": true, "src": true, "action": true,
	"xlink:href": true, "formaction": true,
	"class": true, "title": true, "alt": true, "placeholder": true,
	"disabled": true, "checked": true, "hidden": true, "open": true,
	"selected": true, "tabindex": true, "role": true,
	"width": true, "height": true,
	"data-active": true, "data-state": true, "data-testid": true,
}

// SignalAttrAllowed reports whether a signal binding may write the named
// HTML attribute. Matching is case-insensitive: HTML attribute names are
// case-insensitive to the parser, so `OnClick` is an event handler no
// matter how it was cased.
//
// Exported so both emitters of data-cui-signal-attr, BindAttr here
// and core-ui/store's Slice.BindAttr, enforce the same list instead
// of growing a second copy.
func SignalAttrAllowed(htmlAttr string) bool {
	n := strings.ToLower(htmlAttr)
	if strings.HasPrefix(n, "aria-") {
		return true
	}
	return signalAttrAllowList[n]
}
