package interactive

import "strings"

var signalAttrAllowList = map[string]bool{
	"value": true, "href": true, "src": true, "action": true,
	"xlink:href": true, "formaction": true,
	"class": true, "title": true, "alt": true, "placeholder": true,
	"disabled": true, "checked": true, "hidden": true, "open": true,
	"selected": true, "tabindex": true, "role": true,
	"width": true, "height": true,
	"data-active": true, "data-state": true, "data-testid": true,
}

// SignalAttrAllowed reports whether a signal binding may write the named HTML attribute.
func SignalAttrAllowed(htmlAttr string) bool {
	n := strings.ToLower(htmlAttr)
	if strings.HasPrefix(n, "aria-") {
		return true
	}
	return signalAttrAllowList[n]
}
