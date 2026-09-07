package docs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"

	// The security analyzer self-registers; Run refuses a pass with an
	// empty registry rather than reporting a vacuous clean tree.
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// The contracts rules run over the doc corpus, the sibling of
// TestDocExamplesCompile. Compilation proves a snippet builds; it says
// nothing about what it teaches. i18n.md's "change language" recipe
// compiled fine while teaching a cookie with no HttpOnly/Secure/
// SameSite (GOFASTR1404) and a redirect to a bare r.Referer() — recipes
// are copied verbatim into apps, so the corpus must obey the same
// rules `gofastr verify` enforces on the app itself.
//
// The pass runs over exactly the files TestDocExamplesCompile assembles
// (one package per compile-marked snippet), through the contracts
// package's own runner (NewPass + Run), so the semantics are the
// binary's, not a re-implementation.
//
// RED BY DESIGN until the docs fix lands: the i18n.md recipe is the
// known offender (framework/docs/content/i18n.md:177-182); the failure
// lines it prints are the recipe's, so the fix and this gate merge
// independently.

func TestDocExamplesPassSecurityRules(t *testing.T) {
	snippets := collectCompileSnippets(t)
	if len(snippets) == 0 {
		t.Fatal("no gofastr:compile directives found — the gate would pass vacuously")
	}

	dir := t.TempDir()
	index := map[string]compileSnippet{}
	for i, s := range snippets {
		pkg := fmt.Sprintf("s%02d", i)
		d := filepath.Join(dir, pkg)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(d, "main.go"), []byte(s.body), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
		index[pkg] = s
	}

	pass, err := contracts.NewPass(dir, nil)
	if err != nil {
		t.Fatalf("contracts pass: %v", err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security"}})
	if err != nil {
		t.Fatalf("contracts run: %v", err)
	}
	for _, d := range report.Diagnostics {
		pkg := filepath.Dir(d.File)
		s, ok := index[pkg]
		where := pkg
		if ok {
			where = fmt.Sprintf("%s (generated from %s)", pkg, s.doc)
		}
		t.Errorf("GATE: doc example fails the security rules — %s %s:%d: %s\n--- snippet (from %s) ---\n%s",
			d.RuleID, where, d.Line, d.Message, s.doc, s.body)
	}
}

// TestDocExamplesNeverRedirectToReferer is the plain-text half of the
// gate: a compile-marked snippet must not teach
// http.Redirect(w, r, r.Referer(), …). Referer is request-controlled,
// so the recipe is an open redirect that also leaks the full URL (query
// strings included) to whatever page sent the traffic; docs teach it as
// the idiomatic "go back" spell, and apps copy recipes verbatim.
func TestDocExamplesNeverRedirectToReferer(t *testing.T) {
	re := regexp.MustCompile(`http\.Redirect\(\s*\w+\s*,\s*\w+\s*,\s*(?:r|req)\.Referer\(\)\s*,`)
	for _, s := range collectCompileSnippets(t) {
		for i, line := range strings.Split(s.snippet, "\n") {
			if re.MatchString(line) {
				t.Errorf("GATE: %s teaches http.Redirect to a bare Referer (snippet line %d): %q — Referer is request-controlled and leaks the full URL; redirect to a fixed route or echo an allow-listed path parameter", s.doc, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// compileSnippet is one compile-marked block: the doc it came from, the
// assembled main.go TestDocExamplesCompile emits for it, and the raw
// snippet text for plain-text assertions.
type compileSnippet struct {
	doc     string
	snippet string
	body    string
}

func collectCompileSnippets(t *testing.T) []compileSnippet {
	t.Helper()
	entries, err := fs.ReadDir(contentFS, "content")
	if err != nil {
		t.Fatalf("read content dir: %v", err)
	}
	var out []compileSnippet
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := fs.ReadFile(contentFS, "content/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range compileDirective.FindAllStringSubmatch(string(body), -1) {
			out = append(out, compileSnippet{
				doc:     e.Name(),
				snippet: m[2],
				body:    assemble(m[1], m[2]),
			})
		}
	}
	return out
}
