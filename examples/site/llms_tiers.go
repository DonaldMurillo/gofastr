// =============================================================================
// llms_tiers.go — the two-tier llms.txt surface over the embedded docs.
//
// Tier 1: /llms.txt — an index built from the same docIntents catalog that
// drives /docs/. Every entry links the RAW markdown at /docs/<slug>.md (not
// the HTML page), so an agent reading the index can curl the source directly.
//
// Tier 2: /llms-full.txt — the whole embedded corpus concatenated into one
// markdown file, for agents that want everything in a single request.
//
// Both are wired into uihost.WithAgentReady in setupServer; the raw
// /docs/<slug>.md routes are registered on the app router right after the
// framework app exists. llms_tiers_test.go pins the parity: every embedded
// doc appears in the index, and every indexed URL resolves.
// =============================================================================

package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// llmsSections builds the /llms.txt index: one section per reading intent
// (same catalog as /docs/), each doc linking its raw markdown, plus the
// full-corpus tier up top and the screen index under Optional.
func llmsSections() []uihost.LLMsTxtSection {
	sections := []uihost.LLMsTxtSection{{
		Title: "Full corpus",
		Links: []uihost.LLMsTxtLink{{
			Name:  "llms-full.txt",
			URL:   "/llms-full.txt",
			Notes: "every doc below concatenated into one markdown file",
		}},
	}}
	for _, it := range docIntents {
		links := make([]uihost.LLMsTxtLink, 0, len(it.Docs))
		for _, d := range it.Docs {
			links = append(links, uihost.LLMsTxtLink{
				Name:  d.Title,
				URL:   "/docs/" + d.Slug + ".md",
				Notes: d.Desc,
			})
		}
		sections = append(sections, uihost.LLMsTxtSection{Title: it.Title, Links: links})
	}
	sections = append(sections, uihost.LLMsTxtSection{
		Title: "Optional",
		Links: []uihost.LLMsTxtLink{{
			Name:  "Site pages",
			URL:   "/llm-pages.md",
			Notes: "markdown index of this site's screens (not the framework docs)",
		}},
	})
	return sections
}

// llmsFullText concatenates the embedded corpus into the /llms-full.txt
// body. Each doc is preceded by a separator naming its raw-markdown URL,
// so an agent can cite or re-fetch a single doc. README (the folder's own
// index, not a page) is skipped, matching /docs/.
func llmsFullText() string {
	topics, err := docs.List()
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# GoFastr — full documentation\n\n")
	b.WriteString("> All " + strconv.Itoa(len(topics)-1) + " embedded framework docs in one file. ")
	b.WriteString("The index tier is /llms.txt; each doc is also served alone at the /docs/<name>.md URL named before it.\n")
	for _, t := range topics {
		if t.Name == "README" {
			continue
		}
		body, err := docs.Get(t.Name)
		if err != nil {
			continue
		}
		b.WriteString("\n\n---\n\n<!-- /docs/" + t.Name + ".md -->\n\n")
		b.Write(body)
	}
	return b.String()
}

// registerDocMarkdownRoutes serves each embedded doc's raw markdown at
// /docs/<name>.md — the URLs /llms.txt links to. The HTML page for the
// same doc stays at /docs/<name>.
func registerDocMarkdownRoutes(r *router.Router) {
	topics, err := docs.List()
	if err != nil {
		return
	}
	for _, t := range topics {
		if t.Name == "README" {
			continue
		}
		name := t.Name
		r.Get("/docs/"+name+".md", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			body, err := docs.Get(name)
			if err != nil {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(body)
		}))
	}
}
