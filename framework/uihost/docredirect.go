package uihost

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func writeDocumentRedirect(w http.ResponseWriter, target string) bool {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Gofastr-Document", escapeNonASCII(scrubCtl(target)))
	w.WriteHeader(http.StatusOK)
	return true
}

func escapeNonASCII(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func escapeLayerKey(s string) string {
	if !strings.ContainsFunc(s, func(character rune) bool {
		return character < 0x20 || character == 0x7f || character == '%' || character >= 0x80
	}) {
		return s
	}
	var encoded strings.Builder
	for index := range len(s) {
		character := s[index]
		if character < 0x20 || character == 0x7f || character == '%' || character >= 0x80 {
			fmt.Fprintf(&encoded, "%%%02X", character)
		} else {
			encoded.WriteByte(character)
		}
	}
	return encoded.String()
}

func scriptFetch(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Mode") {
	case "cors", "same-origin":
		return true
	}
	return false
}

func carryQuery(target, rawQuery string) string {
	if rawQuery == "" {
		return target
	}
	rest, frag, hasFrag := strings.Cut(target, "#")
	path, own, _ := strings.Cut(rest, "?")
	ownKeys := map[string]bool{}
	pairs := make([]string, 0, 4)
	for pair := range strings.SplitSeq(own, "&") {
		if pair == "" {
			continue
		}
		ownKeys[queryKey(pair)] = true
		pairs = append(pairs, pair)
	}
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" || ownKeys[queryKey(pair)] {
			continue
		}
		pairs = append(pairs, pair)
	}
	out := path
	if len(pairs) > 0 {
		out += "?" + strings.Join(pairs, "&")
	}
	if hasFrag {
		out += "#" + frag
	}
	return out
}

func queryKey(pair string) string {
	k, _, _ := strings.Cut(pair, "=")
	if dk, err := url.QueryUnescape(k); err == nil {
		return dk
	}
	return k
}
