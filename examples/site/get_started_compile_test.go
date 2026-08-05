package main

// Executable-docs gate for the /get-started page.
//
// The Get Started page is the most-traveled adoption path, and its two Go
// code blocks (the sample entity declaration and the "add a page" screen)
// are the first Go a reader pastes. They drifted before: the entity sample
// used a top-level `CRUD:` field after that field moved onto
// entity.ExposureConfig, so the sample stopped compiling as Go and nobody
// noticed — there was no test rendering the page and compiling what it shows.
//
// This gate renders GetStartedScreen, extracts every Go code block (the
// ui.CodeBlock samples — the terminal blocks are shell, not Go), wraps each
// snippet in a minimal harness that supplies the package context + imports a
// reader would already have, and compiles it against the local tree via a
// replace directive. A snippet that drifts from the framework's real API
// fails CI here, not in a reader's editor.
//
// Red-first: this test was committed against the broken `CRUD:` sample and
// watched fail before the fix landed.

import (
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const gsGoFastrModule = "github.com/DonaldMurillo/gofastr"

func TestGetStartedGoBlocksCompile(t *testing.T) {
	page := string((&GetStartedScreen{}).Render())

	blocks := extractGetStartedGoBlocks(t, page)
	if len(blocks) == 0 {
		t.Fatal("no Go code blocks found on /get-started — extraction is broken or the page changed shape")
	}

	for _, b := range blocks {
		b := b
		t.Run(b.label, func(t *testing.T) {
			src, ok := wrapGetStartedSnippet(b.filename, b.source)
			if !ok {
				t.Fatalf("no compile harness for code block %q — add one in wrapGetStartedSnippet", b.filename)
			}
			compileGetStartedSnippet(t, src, b.label)
		})
	}
}

type gsCodeBlock struct {
	filename string // from the CodeBlock chrome header
	label    string // subtest name (base filename)
	source   string // tag-stripped Go source, one line per rendered line
}

// extractGetStartedGoBlocks finds every framed ui.CodeBlock whose filename
// ends in .go and returns the tag-stripped source. Non-Go blocks (yaml,
// shell terminal output) are skipped. The CodeBlock chrome emits a stable
// pair — a <span class="ui-code-block__file">NAME</span> header and a
// <pre class="ui-code-block__body">…</pre> body — in document order; we pair
// them by count. If the framework reshapes that chrome the count check fails
// loudly, which is the intent: the gate must track what the page renders.
func extractGetStartedGoBlocks(t *testing.T, page string) []gsCodeBlock {
	t.Helper()
	fileRe := regexp.MustCompile(`<span class="ui-code-block__file">(.*?)</span>`)
	preRe := regexp.MustCompile(`(?s)<pre[^>]*class="ui-code-block__body"[^>]*>(.*?)</pre>`)
	files := fileRe.FindAllStringSubmatch(page, -1)
	pres := preRe.FindAllStringSubmatch(page, -1)
	if len(files) != len(pres) {
		t.Fatalf("code-block chrome shape changed: %d file headers vs %d body <pre> blocks", len(files), len(pres))
	}
	var out []gsCodeBlock
	for i := range files {
		name := html.UnescapeString(gsStripTags(files[i][1]))
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		lines := gsSplitLines(pres[i][1])
		out = append(out, gsCodeBlock{
			filename: name,
			label:    filepath.Base(name),
			source:   strings.Join(lines, "\n"),
		})
	}
	return out
}

var gsTagRe = regexp.MustCompile(`<[^>]*>`)

func gsStripTags(s string) string { return gsTagRe.ReplaceAllString(s, "") }

// gsSplitLines reverses the CodeBlock line wrapping back into source lines.
// Each rendered line is wrapped in <span class="ui-code-block__line">…</span>;
// tokens inside are further <span class="tk-*">text</span> spans (or escaped
// plain text). Splitting on the line-wrapper open tag, then stripping every
// remaining tag and unescaping entities, reconstructs the exact Go source —
// the token palette only adds styling, never alters text.
func gsSplitLines(body string) []string {
	const lineOpen = `<span class="ui-code-block__line">`
	parts := strings.Split(body, lineOpen)
	lines := make([]string, 0, len(parts))
	for _, p := range parts[1:] { // [0] is the preamble before the first line
		line := html.UnescapeString(gsStripTags(p))
		line = strings.ReplaceAll(line, "\u200b", "") // blank-line zero-width space
		lines = append(lines, line)
	}
	return lines
}

// wrapGetStartedSnippet wraps an extracted snippet in the package context +
// imports a reader pasting it would already have, keyed by the block's
// filename. The entity snippet is a bare app.Entity(...) statement, so it
// needs a host function; the screen snippet is a type + methods, so it just
// needs a package and imports. Adding a new Go block to the page means adding
// a case here — the default returns false so a missing harness fails loudly
// instead of silently compiling the wrong thing.
func wrapGetStartedSnippet(filename, source string) (string, bool) {
	switch {
	case strings.HasSuffix(filename, "entities.go"):
		// Mirrors entities/entities.go as `gofastr init` writes it: the
		// declaration lives in package entities next to a boolPtr helper.
		return "package entities\n\n" +
			"import (\n" +
			"\t\"github.com/DonaldMurillo/gofastr/core/schema\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework/entity\"\n" +
			")\n\n" +
			"func boolPtr(b bool) *bool { return &b }\n\n" +
			"func registerSample(app *framework.App) {\n" +
			source + "\n" +
			"}\n", true
	case strings.HasSuffix(filename, "about.go"):
		// A screen is a package main type whose Render returns markup.
		return "package main\n\n" +
			"import (\n" +
			"\t\"github.com/DonaldMurillo/gofastr/core/render\"\n" +
			"\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n" +
			")\n\n" +
			source + "\n\n" +
			"func main() {}\n", true
	default:
		return "", false
	}
}

// compileGetStartedSnippet writes the wrapped source into a throwaway module
// whose go.mod points the framework at the local working tree via a replace
// directive (plus a copied go.sum so transitive deps resolve offline), then
// builds it. Mirrors the mechanism in cmd/gofastr/readme_quickstart_test.go
// and init_reliability_test.go.
func compileGetStartedSnippet(t *testing.T, src, label string) {
	t.Helper()
	repoRoot := gsRepoRoot(t)
	goVer, err := gsGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module getstarted-gate\n\ngo " + goVer + "\n\nrequire " + gsGoFastrModule + " v0.0.0\n\nreplace " + gsGoFastrModule + " => " + repoRoot + "\n"
	gsWriteFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := gsCopyFile(filepath.Join(repoRoot, "go.sum"), filepath.Join(dir, "go.sum")); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	gsWriteFile(t, filepath.Join(dir, "main.go"), src)

	build := exec.Command("go", "build", "-mod=mod", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("/get-started code block %q did not compile against the local tree:\n%v\n--- source ---\n%s\n--- build output ---\n%s",
			label, err, src, out)
	}
}

func gsRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func gsGoVersion(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "go" {
			return f[1], nil
		}
	}
	return "", os.ErrNotExist
}

func gsWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gsCopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
