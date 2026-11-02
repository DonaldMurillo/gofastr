package a

import (
	"net/http"
	"net/url"
	"strings"
)

// headerHasToken is core/stream's pre-df472714 helper: it splits its
// value parameter, so a Get passed to it is split.
func headerHasToken(value, token string) bool {
	for part := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// headerValuesHaveToken is the fix spelling: every field line.
func headerValuesHaveToken(values []string, token string) bool {
	for _, v := range values {
		if headerHasToken(v, token) {
			return true
		}
	}
	return false
}

// ---- fires ------------------------------------------------------------

// df472714, pre-fix: core/stream's handshake and subprotocol pick.
func CheckUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") { // want `listheader: Header.Get\("Upgrade"\) reads only the first field line`
		return false
	}
	return headerHasToken(r.Header.Get("Connection"), "upgrade") // want `listheader: Header.Get\("Connection"\) reads only the first field line`
}

func PickSubprotocol(r *http.Request, prefs []string) string {
	raw := r.Header.Get("Sec-WebSocket-Protocol") // want `listheader: Header.Get\("Sec-WebSocket-Protocol"\)`
	if raw == "" {
		return ""
	}
	for p := range strings.SplitSeq(raw, ",") {
		for _, want := range prefs {
			if strings.TrimSpace(p) == want {
				return want
			}
		}
	}
	return ""
}

// Response headers, any case, an equality against a token, and an if
// init binding.
func Responses(w http.ResponseWriter, resp *http.Response) bool {
	if strings.Contains(w.Header().Get("cache-control"), "no-store") { // want `listheader: Header.Get\("cache-control"\)`
		return true
	}
	if resp.Header.Get("Vary") == "*" { // want `listheader: Header.Get\("Vary"\)`
		return true
	}
	if ae := resp.Header.Get("Accept-Encoding"); strings.Contains(ae, "gzip") { // want `listheader: Header.Get\("Accept-Encoding"\)`
		return true
	}
	return false
}

// ---- quiet ------------------------------------------------------------

// The fix spellings.
func Fixed(r *http.Request) bool {
	if !headerValuesHaveToken(r.Header.Values("Upgrade"), "websocket") {
		return false
	}
	joined := strings.Join(r.Header.Values("Sec-WebSocket-Protocol"), ",")
	_ = strings.Split(joined, ",")
	return headerValuesHaveToken(r.Header["Connection"], "upgrade")
}

// Presence tests, a value passed on whole, and a single-valued header.
func Quiet(r *http.Request, resp *http.Response) string {
	if r.Header.Get("Upgrade") == "" || r.Header.Get("Connection") != "" {
		return ""
	}
	cc := resp.Header.Get("Cache-Control")
	if cc != "" {
		resp.Header.Set("X-Upstream-Cache-Control", cc)
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return ""
	}
	return url.PathEscape(resp.Header.Get("Upgrade"))
}

// Near miss: url.Values has a Get too, and a query parameter named
// "connection" is not a header.
func Query(q url.Values) bool {
	return headerHasToken(q.Get("Connection"), "upgrade")
}
