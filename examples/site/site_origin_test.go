package main

import (
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The site declares one public origin (siteOrigin) that drives og:url, the
// sitemap, robots.txt's Sitemap line, the per-page canonicals, and every
// agent-ready discovery URL. Nothing in the page-level test suite could catch
// that origin pointing somewhere the site is not served from: each page was
// internally consistent with the wrong host, so every assertion passed while
// every emitted URL 404'd for a real crawler.
//
// The deployment's true origin is expressed in .github/workflows/pages.yml.
// GitHub Pages serves this repo at a project URL, and the export step passes
// the matching `--export-base <path>`. These two must agree; when they don't,
// the site advertises URLs that resolve to nothing.

var exportBaseRe = regexp.MustCompile(`--export-base[ =]+(/[A-Za-z0-9._-]*)`)

func TestSiteOriginMatchesDeployedBase(t *testing.T) {
	wf, err := os.ReadFile("../../.github/workflows/pages.yml")
	if err != nil {
		t.Fatalf("read pages.yml: %v", err)
	}
	m := exportBaseRe.FindSubmatch(commandLinesOnly(wf))
	if m == nil {
		// Not a skip: the reconciliation disappearing is exactly the drift this
		// test exists to catch. If the deploy stops passing --export-base, the
		// origin below has to be re-derived deliberately, not left unchecked.
		t.Fatal("pages.yml passes no --export-base — siteOrigin can no longer be reconciled against the deployment; update this test with the new source of truth")
	}
	base := string(m[1])

	if !strings.HasPrefix(siteOrigin, "https://") {
		t.Fatalf("siteOrigin must be an absolute https origin, got %q", siteOrigin)
	}
	if strings.HasSuffix(siteOrigin, "/") {
		t.Errorf("siteOrigin must not end in a slash (callers append their own): %q", siteOrigin)
	}
	if base != "/" && !strings.HasSuffix(siteOrigin, base) {
		t.Errorf("siteOrigin %q does not end with the deployed base path %q from pages.yml —\n"+
			"the site would advertise canonical/sitemap/og URLs under an origin it is not served from",
			siteOrigin, base)
	}
}

// The reconciliation above constrains only the PATH. Without this, any host
// serving the right sub-path passed, the origin could point at a domain the
// project does not control and nothing would notice.
func TestSiteOriginHostMatchesTheDeployment(t *testing.T) {
	u, err := url.Parse(siteOrigin)
	if err != nil {
		t.Fatalf("siteOrigin %q is not a URL: %v", siteOrigin, err)
	}
	if u.Scheme != "https" {
		t.Errorf("siteOrigin scheme = %q, want https", u.Scheme)
	}
	// GitHub Pages serves a project site from <owner>.github.io. A custom
	// domain is a deliberate migration: change this expectation in the same
	// commit that changes the deployment, so the two cannot drift silently.
	const wantHost = "donaldmurillo.github.io"
	if u.Host != wantHost {
		t.Errorf("siteOrigin host = %q, want %q — if the deployment moved, update this test and the repolint front-door constant together", u.Host, wantHost)
	}
}

// And it is never left as a placeholder nobody resolved.
func TestSiteOriginIsNotAPlaceholder(t *testing.T) {
	for _, bad := range []string{"example.com", "localhost", "127.0.0.1", "TODO", "changeme"} {
		if strings.Contains(strings.ToLower(siteOrigin), bad) {
			t.Errorf("siteOrigin %q contains placeholder %q", siteOrigin, bad)
		}
	}
}

// commandLinesOnly strips YAML comment lines so the reconciliation reads the
// workflow's actual command and not the prose above it. pages.yml explains
// --export-base in a comment BEFORE invoking it, and matching the first
// occurrence validated the explanation, the command could change underneath
// and this test would still pass.
func commandLinesOnly(b []byte) []byte {
	var out []byte
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}
