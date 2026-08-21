package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type finding struct {
	File    string
	Line    int
	Rule    string
	Message string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	findings, err := lintRepo(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repolint: %v\n", err)
		os.Exit(1)
	}
	if len(findings) == 0 {
		fmt.Println("  ok repo lint clean")
		return
	}
	fmt.Fprintf(os.Stderr, "  found %d repo lint issue(s):\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "  %s:%d [%s] %s\n", f.File, f.Line, f.Rule, f.Message)
	}
	os.Exit(1)
}

func lintRepo(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name(), path == root) {
				return fs.SkipDir
			}
			return nil
		}
		if name := d.Name(); isProcessArtifactMarkdown(name) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			findings = append(findings, finding{
				File:    filepath.ToSlash(rel),
				Line:    1,
				Rule:    "process-artifact-markdown",
				Message: "process ledgers/journals/handoffs don't live as tracked markdown — the rationale goes in commit messages and pinning tests; the history IS git history",
			})
		}
		if name := d.Name(); hasControlChar(name) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			// Don't ToSlash — a newline in the name would render the path
			// unreadable; quote it so the finding is legible.
			findings = append(findings, finding{
				File:    strconv.Quote(filepath.ToSlash(rel)),
				Line:    1,
				Rule:    "bad-filename",
				Message: "file name contains a control character (likely a botched edit artifact)",
			})
		}
		if !isLintedFile(path) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		findings = append(findings, lintBytes(rel, body)...)
		if strings.HasSuffix(path, ".go") {
			findings = append(findings, lintGoSyntax(rel, path, body)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	truthFindings, err := lintRepositoryTruth(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, truthFindings...)
	rootMDFindings, err := lintRootMarkdown(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, rootMDFindings...)
	graphFindings, err := lintConsumerModuleGraph(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, graphFindings...)
	doorFindings, err := lintFrontDoor(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, doorFindings...)
	originFindings, err := lintExampleOrigins(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, originFindings...)
	binFindings, err := lintCommandBinariesIgnored(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, binFindings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings, nil
}

func shouldSkipDir(name string, isRoot bool) bool {
	if isRoot {
		return false
	}
	switch name {
	case ".git", "vendor", "node_modules", "dist", "bin", "build", "tmp":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func isLintedFile(path string) bool {
	name := filepath.Base(path)
	switch name {
	case "Makefile", "Dockerfile", "go.mod", "go.sum":
		return true
	}
	switch filepath.Ext(path) {
	case ".go", ".md", ".sh", ".yml", ".yaml", ".json", ".css", ".js", ".ts", ".tsx", ".html":
		return true
	default:
		return false
	}
}

func lintBytes(rel string, body []byte) []finding {
	var out []finding
	if bytes.Contains(body, []byte("\r\n")) {
		out = append(out, finding{
			File:    rel,
			Line:    1,
			Rule:    "crlf",
			Message: "file uses CRLF line endings",
		})
	}
	if line := duplicateURLGuardLine(rel, body); line > 0 {
		out = append(out, finding{
			File:    rel,
			Line:    line,
			Rule:    "duplicate-url-guard",
			Message: "re-derived URL-scheme allow-list — call core-ui/urlsafe (urlsafe.OK / urlsafe.Clean) instead of growing another copy",
		})
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if isConflictMarker(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "conflict-marker",
				Message: "merge conflict marker left in source",
			})
		}
		if isBuildScript(rel) && mentionsExternalLintTool(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "external-lint-tool",
				Message: "build linting must use repo-owned checks or Go-team tools only",
			})
		}
		if rel == "go.mod" && mentionsExternalLintDependency(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "external-lint-dependency",
				Message: "lint dependencies must stay repo-owned or Go-team tools only",
			})
		}
		if isBuildScript(rel) && strings.Contains(line, "framework/apiversions") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "retired-package-path",
				Message: "framework/apiversions moved to framework/experimental/apiversions",
			})
		}
		// untyped-fui-wiring: the blueprint generator and admin battery
		// EMIT island wiring, so they must teach the typed
		// core-ui/interactive layer (interactive.OnClick/OnSubmit or
		// Action.Attrs()), never a raw "data-fui-rpc" attribute literal.
		// Scoped narrowly — examples/site legitimately renders raw
		// data-fui-* attrs in doc copy shown to visitors, and the
		// pattern data-fui-rpc" matches only the bare rpc attribute
		// (not -method/-body/-close/-reset/-navigate/-signal).
		if isUntypedFUIWiringScope(rel) && strings.Contains(line, `data-fui-rpc"`) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "untyped-fui-wiring",
				Message: `emit RPC wiring via core-ui/interactive (interactive.OnClick/OnSubmit or Action.Attrs()), not a raw "data-fui-rpc" attribute literal`,
			})
		}
		if isBuildScript(rel) && strings.Contains(line, ".pi/worktrees/roadmap") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "worktree-specific-script",
				Message: "build scripts must resolve from the repository root, not a personal worktree path",
			})
		}
		if rel == "Makefile" && strings.Contains(line, "No codegen yet") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "stale-codegen-status",
				Message: "GoFastr ships blueprint code generation; keep the generate target truthful",
			})
		}
	}
	return out
}

// testOnlyModules are dependencies that exist purely to run GoFastr's own
// tests. They must never appear in the root go.mod, because a require there is
// inherited by every application that imports the framework: Go records a
// checksum for each of the dependency's requirements, so `go mod tidy` in a
// hello-world app resolves them all. Measured before this rule existed, the
// testcontainers require dragged the whole Docker client stack — go-winio,
// go-ansiterm, plan9stats, perfstat, purego, wmi, go-ole — into a scaffold
// that never starts a container.
//
// The escape route is not a build tag: `go mod tidy` considers every build
// configuration, so a tagged file's imports are recorded anyway. A test-only
// dependency has to be absent from the module's packages entirely — reached
// through an env var the CI lane supplies, or moved to a nested module.
//
// chromedp and lib/pq deliberately are NOT listed: battery/print renders PDFs
// with the former and framework/fanout speaks Postgres with the latter, so both
// are genuine runtime dependencies of code an app can import.
var testOnlyModules = []string{
	"github.com/testcontainers/testcontainers-go",
}

// lintConsumerModuleGraph keeps test-only dependencies out of the root go.mod.
func lintConsumerModuleGraph(root string) ([]finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []finding
	// Only `require` entries put a module in a consumer's graph. A `replace`
	// line does not, and matching one would report a finding that cannot be
	// acted on. Track which block each line sits in, and handle both the
	// parenthesised block and the single-line `require x v1` form.
	inRequireBlock := false
	inOtherBlock := false
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		switch {
		case trimmed == "require (":
			inRequireBlock = true
			continue
		case strings.HasSuffix(trimmed, "("):
			// replace ( / exclude ( / retract (
			inOtherBlock = true
			continue
		case trimmed == ")":
			inRequireBlock, inOtherBlock = false, false
			continue
		}
		if inOtherBlock {
			continue
		}
		entry := trimmed
		if !inRequireBlock {
			rest, ok := strings.CutPrefix(trimmed, "require ")
			if !ok {
				continue
			}
			entry = strings.TrimSpace(rest)
		}
		// The module path is the first field; the version and any
		// `// indirect` marker follow it.
		path, _, _ := strings.Cut(entry, " ")
		for _, mod := range testOnlyModules {
			// Exact match, or a submodule of it — testcontainers-go and
			// testcontainers-go/modules/postgres are both the same problem,
			// while a hypothetical testcontainers-go-helpers is not.
			if path != mod && !strings.HasPrefix(path, mod+"/") {
				continue
			}
			out = append(out, finding{
				File:    "go.mod",
				Line:    i + 1,
				Rule:    "test-only-dep-in-consumer-graph",
				Message: mod + " is test-only; a require here is inherited by every app that imports GoFastr. Supply it through TEST_POSTGRES_DSN in CI, or move it to a nested module",
			})
		}
	}
	return out, nil
}

func lintRepositoryTruth(root string) ([]finding, error) {
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	minor := latestReleaseMinor(string(changelog))
	if minor == "" {
		return nil, nil
	}
	security, err := os.ReadFile(filepath.Join(root, "SECURITY.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	expected := "currently `" + minor + ".x`"
	if strings.Contains(string(security), expected) {
		return nil, nil
	}
	return []finding{{
		File:    "SECURITY.md",
		Line:    lineContaining(string(security), "currently `"),
		Rule:    "supported-version-drift",
		Message: "supported release must match latest CHANGELOG release (want " + minor + ".x)",
	}}, nil
}

// lintFrontDoor keeps the README pointing at the deployed docs site.
//
// The site is the project's best asset and was, for months, reachable from
// nowhere: the repo homepage field pointed at pkg.go.dev and the README — the
// only page a stranger actually lands on — contained zero links to it. Every
// individual artifact was fine; the path between them did not exist. A
// published site nobody can navigate to is indistinguishable from no site.
func lintFrontDoor(root string) ([]finding, error) {
	// The rule encodes a fact about THIS repository's front door, so it applies
	// only to this module. repolint is otherwise a general-purpose tree linter
	// (its own tests run it over synthetic trees), and a rule demanding a
	// specific README link would fail every one of them.
	isThisRepo, err := moduleIs(root, frameworkModule)
	if err != nil || !isThisRepo {
		return nil, err
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		if os.IsNotExist(err) {
			// A missing README is the strongest version of the failure this
			// rule exists to prevent, not an exemption from it. Returning nil
			// here would have let `rm README.md` pass the gate clean.
			return []finding{{
				File:    "README.md",
				Line:    1,
				Rule:    "front-door-missing",
				Message: "README.md does not exist — it is the only page a stranger lands on",
			}}, nil
		}
		return nil, err
	}
	// The origin the site is actually deployed to, as declared by the Pages
	// workflow's export base. Kept as a literal here (rather than importing
	// the site package) so the lint stays a standalone stdlib binary.
	const siteURL = "https://donaldmurillo.github.io/gofastr"
	if strings.Contains(string(readme), siteURL) {
		return nil, nil
	}
	return []finding{{
		File:    "README.md",
		Line:    1,
		Rule:    "front-door-missing",
		Message: "README must link the deployed docs site (" + siteURL + ") — a site nothing links to is unreachable",
	}}, nil
}

func latestReleaseMinor(changelog string) string {
	for _, line := range strings.Split(changelog, "\n") {
		if !strings.HasPrefix(line, "## [") || strings.HasPrefix(line, "## [Unreleased]") {
			continue
		}
		end := strings.Index(line, "]")
		if end < len("## [") {
			continue
		}
		parts := strings.Split(line[len("## ["):end], ".")
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
	}
	return ""
}

func lineContaining(body, needle string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 1
}

// rootMarkdownAllowlist is the complete set of markdown files permitted
// at the repository root: the GitHub-recognized community-health files
// plus the roadmap and the agent entry points. Everything else is a
// process artifact that belongs in git history, a gate, or a skill —
// not on the repo's front porch.
var rootMarkdownAllowlist = map[string]bool{
	"README.md":          true,
	"CHANGELOG.md":       true,
	"CONTRIBUTING.md":    true,
	"SECURITY.md":        true,
	"CODE_OF_CONDUCT.md": true,
	"SUPPORT.md":         true,
	"ROADMAP.md":         true,
	"CLAUDE.md":          true,
	"AGENTS.md":          true,
}

func lintRootMarkdown(root string) ([]finding, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
			continue
		}
		if !rootMarkdownAllowlist[name] {
			out = append(out, finding{
				File:    name,
				Line:    1,
				Rule:    "root-markdown",
				Message: "not in the root markdown allowlist — audits/journals/plans go to git history or an enforceable gate, feature docs go to framework/docs/content/",
			})
		}
	}
	return out, nil
}

// isProcessArtifactMarkdown flags ALL-CAPS markdown names built around
// process-ledger tokens (AUDIT, FINDINGS, NOTES, JOURNAL, HANDOFF,
// LEDGER) anywhere in the tree. Feature docs are lowercase (audit-log.md
// is fine); the SCREAMING_SNAKE ledger genre is what accretes.
func isProcessArtifactMarkdown(name string) bool {
	if !strings.HasSuffix(name, ".md") {
		return false
	}
	stem := strings.TrimSuffix(name, ".md")
	if stem == "" || stem != strings.ToUpper(stem) {
		return false
	}
	for _, tok := range []string{"AUDIT", "FINDINGS", "NOTES", "JOURNAL", "HANDOFF", "LEDGER"} {
		if strings.Contains(stem, tok) {
			return true
		}
	}
	return false
}

// hasControlChar reports whether s contains any ASCII control byte
// (including newline/tab/CR). Legitimate file names never do; a botched
// multi-line edit that lands a prompt fragment as a filename does.
func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func isConflictMarker(line string) bool {
	return strings.HasPrefix(line, "<<<<<<< ") ||
		strings.HasPrefix(line, "=======") ||
		strings.HasPrefix(line, ">>>>>>> ")
}

func isBuildScript(rel string) bool {
	if rel == "Makefile" {
		return true
	}
	return strings.HasPrefix(rel, "scripts/") && strings.HasSuffix(rel, ".sh")
}

// isUntypedFUIWiringScope reports whether rel is a non-test Go file under
// the blueprint generator or a battery — the code that EMITS island wiring
// and so must use the typed core-ui/interactive layer instead of raw
// "data-fui-rpc" attribute literals. Examples and docs are deliberately
// excluded: the showcase renders raw data-fui-* attributes in copy shown
// to visitors.
func isUntypedFUIWiringScope(rel string) bool {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false
	}
	return strings.HasPrefix(rel, "cmd/gofastr/") || strings.HasPrefix(rel, "battery/")
}

// duplicateURLGuardLine reports the line of a re-derived URL-scheme
// allow-list, or 0 when the file has none.
//
// The fingerprint is the percent-encoded CR/LF rejection, which every copy of
// this guard carries and almost nothing else does. The guard had been written
// five times — framework/ui, framework/uihost, framework/crud,
// framework/experimental/apiversions and three core-ui/patterns builders —
// each copy byte-identical on the day it was written and free to drift the
// day after. core-ui/urlsafe is the one definition; this rule is what keeps
// it the only one.
func duplicateURLGuardLine(rel string, body []byte) int {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return 0
	}
	if strings.HasPrefix(rel, "core-ui/urlsafe/") {
		return 0 // the definition itself
	}
	if strings.HasPrefix(rel, "cmd/repolint/") {
		return 0 // this rule naming the pattern is not a copy of it
	}
	if !bytes.Contains(body, []byte(`"%0d"`)) || !bytes.Contains(body, []byte(`"%0a"`)) {
		return 0
	}
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, `"%0d"`) {
			return i + 1
		}
	}
	return 1
}

func mentionsExternalLintTool(line string) bool {
	return strings.Contains(line, "golangci-lint") ||
		strings.Contains(line, "staticcheck")
}

func mentionsExternalLintDependency(line string) bool {
	for _, mod := range []string{
		"github.com/golangci/golangci-lint",
		"honnef.co/go/tools",
		"mvdan.cc/gofumpt",
	} {
		if strings.Contains(line, mod) {
			return true
		}
	}
	return false
}

func lintGoSyntax(rel, path string, body []byte) []finding {
	if isGeneratedGo(body) {
		return nil
	}
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, path, body, parser.SkipObjectResolution)
	if err == nil {
		return nil
	}
	line := 1
	if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
		line = list[0].Pos.Line
	}
	return []finding{{
		File:    rel,
		Line:    line,
		Rule:    "go-syntax",
		Message: err.Error(),
	}}
}

func isGeneratedGo(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("// Code generated")) ||
		bytes.Contains(head, []byte("DO NOT EDIT"))
}

// exampleAbsoluteURLRe finds absolute https URLs written as Go string literals.
var exampleAbsoluteURLRe = regexp.MustCompile(`(?i)https://([A-Za-z0-9._-]+)`)

// allowedExampleHost reports whether an example app may advertise host as its
// own public origin or reference it in emitted markup.
//
// Reserved documentation domains (RFC 2606 / RFC 6761) are always fine: they
// are guaranteed never to resolve to a real service, which is exactly what a
// demo product's canonical URLs should say. Spec and schema hosts are
// references, not claims about where the app lives. Everything else must be a
// host the project actually serves from.
func allowedExampleHost(host string) bool {
	switch host {
	case "example.com", "example.net", "example.org",
		// Loopback in any spelling — "localhost" was allowed while the
		// equivalent literal address was not, which flagged correct code.
		"localhost", "127.0.0.1", "::1",
		"schema.org", "www.schema.org", "www.sitemaps.org", "www.w3.org",
		"llmstxt.org", "spec.modelcontextprotocol.io", "modelcontextprotocol.io",
		"pkg.go.dev", "go.dev", "github.com", "donaldmurillo.github.io",
		"barcode.donaldmurillo.com", "img.shields.io", "keepachangelog.com",
		"semver.org", "creativecommons.org", "opensource.org", "www.rfc-editor.org":
		return true
	}
	// RFC 2606 reserves the .example TLD as well as the example.{com,net,org}
	// second-level names, so "notes.example" is as legitimately illustrative as
	// "notes.example.com".
	return strings.HasSuffix(host, ".example") ||
		strings.HasSuffix(host, ".example.com") ||
		strings.HasSuffix(host, ".example.net") ||
		strings.HasSuffix(host, ".example.org")
}

// lintExampleOrigins keeps example apps off hostnames that do not resolve.
//
// The site advertised canonical URLs, a sitemap, and an agent card on a domain
// with no DNS record for months. Fixing that one file by name left the same bug
// one directory over in the flagship example, on a sibling of the same dead
// domain — which is the whole lesson: the defect is not a string, it is
// "an example claims an origin nobody serves". This rule enumerates by that
// invariant, so the next one fails the build instead of waiting to be noticed.
//
// A demo that is not deployed should say so with a reserved documentation
// domain; a deployed one names the host it actually answers on.
func lintExampleOrigins(root string) ([]finding, error) {
	var findings []finding
	examples := filepath.Join(root, "examples")
	err := filepath.WalkDir(examples, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		seen := map[string]bool{}
		var lex goLexState
		for i, raw := range strings.Split(string(body), "\n") {
			// A URL discussed in a comment is prose, not something the example
			// emits, so only the code on the line is scanned. One pass strips
			// both comment forms, because the three lexical contexts define
			// each other away and cannot be recognised in sequence.
			var line string
			line, lex = stripGoComments(raw, lex)
			for _, m := range exampleAbsoluteURLRe.FindAllStringSubmatch(line, -1) {
				host := m[1]
				if allowedExampleHost(host) || seen[host] {
					continue
				}
				seen[host] = true
				findings = append(findings, finding{
					File:    filepath.ToSlash(rel),
					Line:    i + 1,
					Rule:    "example-dead-origin",
					Message: "example advertises origin " + host + " — use a reserved documentation domain (example.com) for a demo that is not deployed, or a host the project actually serves",
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// frameworkModule is this repository's module path.
const frameworkModule = "github.com/DonaldMurillo/gofastr"

// moduleIs reports whether root's go.mod declares the given module path.
// Absent or unreadable go.mod means "not this module" rather than an error, so
// repolint stays usable on trees that are not Go modules at all.
func moduleIs(root, module string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "module") {
			continue
		}
		rest := strings.TrimSpace(trimmed[len("module"):])
		// `module<TAB>path` is valid go.mod. Requiring a space made the
		// module never match, silently disabling every rule scoped to it.
		if rest == "" || rest == trimmed {
			continue
		}
		// `module path // comment` is valid go.mod. Comparing the whole
		// remainder made the comment part of the path, so the module never
		// matched and every rule scoped to it silently stopped running — a
		// gate that disables itself is worse than no gate.
		if i := strings.Index(rest, "//"); i >= 0 {
			rest = rest[:i]
		}
		return strings.TrimSpace(rest) == module, nil
	}
	return false, nil
}

// goLexState carries the lexical context that survives a newline in Go source:
// a block comment and a raw string literal are the only two constructs that do.
type goLexState struct {
	inBlockComment bool
	inRawString    bool
}

// stripGoComments returns the code on one line of Go source with both comment
// forms removed, plus the lexical state the next line starts in.
//
// The three contexts — line comment, block comment, string literal — have to be
// recognised in a single left-to-right pass, because each one turns the others'
// delimiters into ordinary text. Scanning for one before the others is what
// made the previous version under-report: it looked for "/*" with a plain
// strings.Index before it had considered "//" or a quote, so `// see /* here`
// and `s := "/*"` each opened a block comment that never closed, and every
// remaining line in the file was skipped. A lint that stops reading reports the
// same "no findings" as a lint that read everything and found nothing.
//
// String literals are also what protect "https://host": a "//" inside a string
// is content, not a comment, and a URL in Go source is always inside one. An
// earlier version instead kept any "//" preceded by a colon, on the theory that
// a URL scheme has one and a comment does not — but a label, `case`, or
// `default` may be followed immediately by a comment with no space
// (`default:// note` is valid Go), so that rule left those comments unstripped
// and let prose inside them read as emitted code.
func stripGoComments(line string, st goLexState) (string, goLexState) {
	var code strings.Builder
	var quote byte
	if st.inRawString {
		quote = '`'
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if st.inBlockComment {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				st.inBlockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			code.WriteByte(c)
			if c == '\\' && quote != '`' {
				if i+1 < len(line) {
					code.WriteByte(line[i+1])
				}
				i++ // an escaped quote does not close the literal
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			code.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(line) {
			if line[i+1] == '/' {
				st.inRawString = false
				return code.String(), st
			}
			if line[i+1] == '*' {
				st.inBlockComment = true
				i++
				continue
			}
		}
		code.WriteByte(c)
	}
	// Only a raw string carries across the newline; an interpreted one left
	// open here is a syntax error the compiler reports, not our problem.
	st.inRawString = quote == '`'
	return code.String(), st
}

// lintCommandBinariesIgnored checks that every cmd/<name> has a root-level
// .gitignore entry, because `go build ./cmd/<name>` from the repository root
// drops the binary at /<name>.
//
// .gitignore cannot glob a directory listing, so the entries are written out
// by hand — and a hand-written list of names rots the moment a command is
// added. It rotted twice: a 12MB embed-demo binary reached a commit that way,
// and the very change set that added this rule shipped a 3.4MB /mutate
// untracked, under a comment telling the reader to "enumerate the package dir,
// not the names". This rule is that enumeration.
func lintCommandBinariesIgnored(root string) ([]finding, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	body, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// A missing .gitignore is not "clean" — it ignores nothing, so EVERY
	// command binary would be untracked. Treating absence as clean was the
	// rule's worst blind spot: it read the one state where the problem is
	// total as the one state where there is no problem. body stays nil and
	// every command falls through to the report below.
	ignored := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		// Skipping blanks and comments is tidiness, not correctness: neither
		// can match a command name once the leading "/" is trimmed, so a
		// mutation removing this line changes no verdict. Mutation testing
		// reports it as a survivor for that reason — it is equivalent code,
		// not an untested guard.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ignored[strings.TrimPrefix(line, "/")] = true
	}
	var out []finding
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if ignored[name] {
			continue
		}
		// A command whose name is also a tracked top-level directory is
		// covered by the existing `name` + `!name/` pair; treat that as
		// ignored rather than demanding a duplicate entry.
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr == nil {
			continue
		}
		out = append(out, finding{
			File: ".gitignore",
			Rule: "cmd-binary-not-ignored",
			Message: "cmd/" + name + " builds to /" + name + " but .gitignore has no entry for it — " +
				"a `go build ./cmd/" + name + "` from the root leaves an untracked binary",
		})
	}
	return out, nil
}
