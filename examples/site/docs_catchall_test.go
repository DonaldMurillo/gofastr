package main

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestDocsCatchAllURLSetUnchanged verifies the /docs/{path...} catch-all
// (one registration) enumerates the SAME concrete URLs the old per-slug
// loop produced: one /docs/<slug> per catalog entry, derived via
// StaticPaths the way the static builder and sitemap do.
func TestDocsCatchAllURLSetUnchanged(t *testing.T) {
	site := coreapp.NewApp("parity")
	registerDocsGroup(site)

	// Expected: every flatDocs slug as /docs/<slug>, sorted.
	want := make([]string, 0, len(flatDocs()))
	for _, e := range flatDocs() {
		want = append(want, "/docs/"+e.Slug)
	}
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("flatDocs is empty — parity test has nothing to compare against")
	}

	// Enumerate via Routes() + StaticPaths, the same path the builder walks.
	var got []string
	for _, r := range site.Routes() {
		screen, _, ok := site.Router.Resolve(r.Path)
		if !ok {
			continue
		}
		provider, ok := screen.Component.(coreapp.StaticPathsProvider)
		if !ok {
			continue
		}
		for _, params := range provider.StaticPaths(context.Background()) {
			got = append(got, expandDocPattern(r.Path, params))
		}
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Errorf("docs URL count: got %d, want %d (a doc lost or duplicated)", len(got), len(want))
	}
	for i := 0; i < len(want) && i < len(got); i++ {
		if got[i] != want[i] {
			t.Errorf("docs URL[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// expandDocPattern substitutes each param into a route pattern the way
// the static builder's applyParams does, stripping the catch-all "*".
func expandDocPattern(pattern string, params map[string]string) string {
	parts := strings.Split(pattern, "/")
	for i, seg := range parts {
		if !strings.HasPrefix(seg, ":") {
			continue
		}
		key := coreapp.ParamName(seg)
		if v, ok := params[key]; ok {
			parts[i] = v
		}
	}
	return strings.Join(parts, "/")
}

// TestDocsCatchAllUnknownSlug404s verifies an unknown doc slug is
// rejected (the resolver answers app.ErrNotFound and the screen's Load
// is its first reader), so the site serves its not-found page with a
// 404, preserving the UX the per-slug loop had by not registering
// unknown paths at all.
func TestDocsCatchAllUnknownSlug404s(t *testing.T) {
	site := coreapp.NewApp("parity")
	registerDocsGroup(site)

	_, err := site.RenderPageResult(context.Background(), "/docs/no-such-doc-xyz")
	if !errors.Is(err, coreapp.ErrNotFound) {
		t.Errorf("unknown doc slug: RenderPageResult error = %v, want app.ErrNotFound", err)
	}

	// Through the real server: the status and the site's own 404 page,
	// which echoes the requested path.
	rec := serve(t, http.MethodGet, "/docs/no-such-doc-xyz")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/docs/no-such-doc-xyz: got %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/docs/no-such-doc-xyz") {
		t.Error("/docs/no-such-doc-xyz should render the site's not-found page (it echoes the path)")
	}
}

// TestDocsCatchAllKnownSlugResolves verifies a known slug resolves to the
// DocPageScreen with the right entry wired through SetParams.
func TestDocsCatchAllKnownSlugResolves(t *testing.T) {
	site := coreapp.NewApp("parity")
	registerDocsGroup(site)

	doc := flatDocs()[0]
	res, err := site.RenderPageResult(context.Background(), "/docs/"+doc.Slug)
	if err != nil {
		t.Fatalf("known slug %q should render, got error: %v", doc.Slug, err)
	}
	if !strings.Contains(string(res.HTML), doc.Slug) && !strings.Contains(string(res.HTML), doc.Title) {
		t.Errorf("rendered doc page should reference the doc slug/title; body missing %q/%q", doc.Slug, doc.Title)
	}
}
