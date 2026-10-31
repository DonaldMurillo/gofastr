package main

import (
	"path"
	"regexp"
	"strings"
)

// The docs corpus links the way GitHub and `gofastr docs` read it: a
// sibling doc as entity-declarations.md (a2a.md#tasks with an anchor),
// a repo file relative to framework/docs/content (../../../ROADMAP.md).
// On the site a sibling resolved to /docs/entity-declarations.md, the
// raw-markdown route, and on the static export, which has no .md paths,
// to a 404; a repo path resolved outside /docs entirely. siteDocLinks
// rewrites each relative href in a rendered doc: a sibling to its page,
// /docs/<slug>, and a repo file to its GitHub page. Every other href (a
// scheme, a root path, a bare anchor) is left as written.

// docHref matches an href attribute as core/markdown writes it.
var docHref = regexp.MustCompile(`href="([^"]*)"`)

// docSibling is a sibling doc's file name: the corpus's slug spelling.
var docSibling = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.md$`)

const (
	docsContentDir = "framework/docs/content"
	repoBlobURL    = "https://github.com/DonaldMurillo/gofastr/blob/main/"
)

func siteDocLinks(html string) string {
	return docHref.ReplaceAllStringFunc(html, func(attr string) string {
		href := docHref.FindStringSubmatch(attr)[1]
		if to, ok := siteDocHref(href); ok {
			return `href="` + to + `"`
		}
		return attr
	})
}

// siteDocHref is the site's spelling of a corpus href, and false for one
// the site serves as written.
func siteDocHref(href string) (string, bool) {
	if href == "" || strings.HasPrefix(href, "/") || strings.HasPrefix(href, "#") || strings.Contains(href, ":") {
		return "", false
	}
	p, frag, _ := strings.Cut(href, "#")
	if frag != "" {
		frag = "#" + frag
	}
	if docSibling.MatchString(p) {
		return "/docs/" + strings.TrimSuffix(p, ".md") + frag, true
	}
	file := path.Join(docsContentDir, p)
	if file == ".." || strings.HasPrefix(file, "../") {
		return "", false
	}
	return repoBlobURL + file + frag, true
}
