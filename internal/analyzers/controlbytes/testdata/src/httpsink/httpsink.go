// Package httpsink mirrors framework/uihost's writeAgentLinkHeaders:
// the request path flows through a helper, a slice of built links, a
// join, and only then into the Link header. markdownAlternate keeps its
// real name and both spellings: the pre-fix pass-through body (via
// plainAlternate, so both can exist) taints; the fixed byte-filter body
// clears.
package httpsink

import (
	"fmt"
	"net/http"
	"strings"
)

// plainAlternate is pre-fix markdownAlternate: trim and concatenate,
// never inspects bytes — control bytes pass through.
func plainAlternate(path string) string {
	if path == "" || path == "/" {
		return "/llm.md"
	}
	return strings.TrimRight(path, "/") + "/llm.md"
}

// markdownAlternate is the a24928c1 fixed spelling: strips every C0
// byte except tab by walking the value as bytes.
func markdownAlternate(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := range len(path) {
		if c := path[i]; c >= 0x20 || c == '\t' {
			b.WriteByte(c)
		}
	}
	path = b.String()
	if path == "" || path == "/" {
		return "/llm.md"
	}
	return strings.TrimRight(path, "/") + "/llm.md"
}

type host struct{ llmMDPublic bool }

// badLinks is the pre-fix writeAgentLinkHeaders reduced to the shape.
func (h *host) badLinks(w http.ResponseWriter, req *http.Request) {
	var links []string
	if h.llmMDPublic {
		links = append(links, fmt.Sprintf(`<%s>; rel="alternate"; type="text/markdown"`, plainAlternate(req.URL.Path)))
	}
	joined := strings.Join(links, ", ")
	if prev := w.Header().Get("Link"); prev != "" {
		joined = prev + ", " + joined
	}
	w.Header().Set("Link", joined) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// goodLinks is the fixed spelling: markdownAlternate now filters the
// bytes itself, so the taint is cleared inside the helper.
func (h *host) goodLinks(w http.ResponseWriter, req *http.Request) {
	var links []string
	if h.llmMDPublic {
		links = append(links, fmt.Sprintf(`<%s>; rel="alternate"; type="text/markdown"`, markdownAlternate(req.URL.Path)))
	}
	joined := strings.Join(links, ", ")
	if prev := w.Header().Get("Link"); prev != "" {
		joined = prev + ", " + joined
	}
	w.Header().Set("Link", joined)
}

// scrubbedAtSink is the other acceptable spelling: the repo's usual
// scrub-named barrier at the sink.
func (h *host) scrubbedAtSink(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Link", scrubCtl(fmt.Sprintf(`<%s>; rel="alternate"`, plainAlternate(req.URL.Path))))
}

func scrubCtl(s string) string {
	var b strings.Builder
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}
