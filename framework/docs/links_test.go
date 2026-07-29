package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file is the link-and-anchor integrity gate for the embedded docs.
// The docs ship at framework/docs/content/*.md and are embedded into the
// gofastr binary (gofastr docs, the MCP framework_docs_* tools), so a dead
// link or a stale in-text anchor reaches a reader with no editor warning.
// Existing tests only checked two pages; FIX 2 (a documented symbol that
// did not exist) and FIX 4 (relative links one directory short) both
// survived because nothing swept every doc. This gate does.

// linkIssue describes one broken link or anchor found by scanDocLinks.
type linkIssue struct {
	file    string // doc path relative to the test CWD
	line    int    // 1-indexed source line
	target  string // the raw link target as written in the doc
	problem string // why it failed
}

func (i linkIssue) String() string {
	return fmt.Sprintf("%s:%d: %q — %s", i.file, i.line, i.target, i.problem)
}

// ghSlugify mirrors the anchor GitHub renders for a heading (the
// github-slugger algorithm): strip code backticks, drop every character
// that is not a letter, number, underscore, hyphen, or space, lowercase,
// then turn each space into a hyphen WITHOUT collapsing runs. That last
// point is the whole reason this exists: "Service accounts & scoped API
// tokens" slugs to "service-accounts--scoped-api-tokens" (the removed
// "&" leaves two spaces, each becomes a hyphen). A hand-written anchor
// with a single hyphen drifts from that and silently 404s in the TOC.
func ghSlugify(heading string) string {
	s := strings.ToLower(heading)
	s = strings.ReplaceAll(s, "`", "")
	s = nonAnchorChar.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

var (
	nonAnchorChar = regexp.MustCompile(`[^\pL\pN\s_-]`)
	headingLineRE = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
	linkTargetRE  = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	// inline code span — links inside `…` are prose, not links.
	inlineCodeRE = regexp.MustCompile("`[^`]*`")
)

// fenceMarker reports whether a line opens or closes a fenced code block.
func fenceMarker(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// isExternal reports whether a link target is absolute or off-repo and so
// outside this gate (http(s)://, mailto:, or a site-root-absolute "/...").
func isExternal(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "/")
}

// headingsIn returns the set of GitHub heading slugs present in doc text.
func headingsIn(text string) map[string]bool {
	out := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		if m := headingLineRE.FindStringSubmatch(line); m != nil {
			out[ghSlugify(m[1])] = true
		}
	}
	return out
}

// scanDocLinks reads every *.md directly under dir and returns one
// linkIssue per failure:
//   - a relative link whose target file does not resolve from dir, and
//   - an in-document anchor (#slug) that matches no heading in its file.
//
// For a "path.md#slug" link whose target lives under dir, the slug is also
// checked against that target's headings (the same drift, one hop away).
// Fenced code blocks and inline code are skipped, so links inside `…` or a
// ``` block are not treated as real links.
func scanDocLinks(dir string) ([]linkIssue, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	// Cache every doc's heading slugs, keyed by path relative to dir.
	headings := make(map[string]string) // rel path -> raw text
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		headings[name] = string(b)
	}
	slugCache := make(map[string]map[string]bool, len(names))
	for name, text := range headings {
		slugCache[name] = headingsIn(text)
	}

	var issues []linkIssue
	for _, name := range names {
		lines := strings.Split(headings[name], "\n")
		inFence := false
		for i, raw := range lines {
			if fenceMarker(raw) {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			line := inlineCodeRE.ReplaceAllString(raw, "")
			for _, m := range linkTargetRE.FindAllStringSubmatch(line, -1) {
				target := m[1]
				if isExternal(target) {
					continue
				}
				filePart, frag, _ := strings.Cut(target, "#")
				if filePart == "" {
					// In-document anchor: must match a heading in this doc.
					if frag != "" && !slugCache[name][frag] {
						issues = append(issues, linkIssue{
							file:    filepath.Join(dir, name),
							line:    i + 1,
							target:  target,
							problem: fmt.Sprintf("anchor %q matches no heading in this doc", frag),
						})
					}
					continue
				}
				resolved := filepath.Clean(filepath.Join(dir, filePart))
				if _, err := os.Stat(resolved); err != nil {
					issues = append(issues, linkIssue{
						file:    filepath.Join(dir, name),
						line:    i + 1,
						target:  target,
						problem: fmt.Sprintf("resolves to %q, which does not exist", resolved),
					})
					continue
				}
				// Cross-file anchor: only when the target is another doc
				// inside dir, so an external *.md with a coincident basename
				// is never checked against this dir's heading cache.
				if frag != "" {
					if rel, rerr := filepath.Rel(dir, resolved); rerr == nil && !strings.HasPrefix(rel, "..") {
						if slugs, ok := slugCache[rel]; ok && !slugs[frag] {
							issues = append(issues, linkIssue{
								file:    filepath.Join(dir, name),
								line:    i + 1,
								target:  target,
								problem: fmt.Sprintf("anchor %q matches no heading in %s", frag, filePart),
							})
						}
					}
				}
			}
		}
	}
	return issues, nil
}

// TestContentLinksResolve is the gate: every relative link in the shipped
// docs must reach a real file, and every in-document (and same-dir
// cross-file) anchor must match a real heading slug. Add a doc, link, or
// heading and this fails fast with the file, line, and target that broke.
func TestContentLinksResolve(t *testing.T) {
	issues, err := scanDocLinks("content")
	if err != nil {
		t.Fatalf("scanDocLinks(content): %v", err)
	}
	for _, is := range issues {
		t.Error(is.String())
	}
}

// TestLinkGateCatchesBroken proves the gate fails on the exact shapes it
// claims to catch — a dead file link and a stale in-text anchor — using a
// testdata fixture rather than a file under content/ (which is embedded
// into the shipped binary and served by `gofastr docs`).
func TestLinkGateCatchesBroken(t *testing.T) {
	issues, err := scanDocLinks(filepath.Join("testdata", "linkgate"))
	if err != nil {
		t.Fatalf("scanDocLinks(testdata/linkgate): %v", err)
	}
	// The clean fixture must produce no issues.
	for _, is := range issues {
		if strings.Contains(is.file, "linkgate-clean.md") {
			t.Errorf("clean fixture reported an issue: %s", is)
		}
	}
	// The broken fixture must report one dead file link and one dead anchor.
	wantFile := strings.Contains(joinIssues(issues), `resolves to`)
	wantAnchor := strings.Contains(joinIssues(issues), `matches no heading`)
	if !wantFile {
		t.Error("gate did not flag the dead relative file link in the broken fixture")
	}
	if !wantAnchor {
		t.Error("gate did not flag the stale in-document anchor in the broken fixture")
	}
}

func joinIssues(issues []linkIssue) string {
	var b strings.Builder
	for _, is := range issues {
		b.WriteString(is.String())
		b.WriteByte('\n')
	}
	return b.String()
}
