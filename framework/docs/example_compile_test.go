package docs

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// packageClause detects a snippet that is a complete Go file.
var packageClause = regexp.MustCompile(`(?m)^package\s+\w+`)

// Compiling the Go examples embedded in the guides, so a snippet cannot rot
// into something that does not build. TestDocsAvoidKnownWrongAPIs is the cheap
// denylist half of this; it cannot catch a scope or signature error, which is
// how first-run.md came to use a variable one line before declaring it.
//
// Most fenced blocks are deliberate fragments (three lines of a struct
// literal), so compilation is opt-in per block. Mark a block by putting a
// directive comment immediately above its fence. HTML comments do not render:
//
//	<!-- gofastr:compile
//	import "database/sql"
//	var db *sql.DB
//	-->
//	```go
//	app := framework.NewApp(framework.WithDB(db))
//	```
//
// Directive body lines are emitted into the generated file: `import` lines join
// the snippet's own imports, `stmt:` lines are appended inside func main (use
// them to consume an otherwise-unused variable), and everything else is a
// top-level declaration used to stub identifiers the snippet assumes.
//
// The snippet's own import block is kept verbatim, so the imports a reader
// copies are compiled too.

// compileDirective matches the directive comment plus the ```go fence that
// follows it. Group 1 is the directive body, group 2 the snippet.
var compileDirective = regexp.MustCompile(
	"(?s)<!--\\s*gofastr:compile\\s*\n(.*?)-->\\s*\n```go\n(.*?)\n```")

// goFence counts every fenced Go block so the test can report opt-in coverage
// instead of silently implying the whole corpus is compiled.
var goFence = regexp.MustCompile("(?m)^```go\\s*$")

type snippet struct {
	doc  string
	body string // assembled main.go
}

// splitImports separates a leading `import ( … )` block from the rest of a
// snippet. Imports must precede declarations in the generated file, and the
// remaining statements have to move inside func main.
func splitImports(src string) (imports, rest string) {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "import (") {
			start = i
		}
		break // only a leading import block counts
	}
	if start < 0 {
		return "", src
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == ")" {
			return strings.Join(lines[start:i+1], "\n"),
				strings.Join(lines[i+1:], "\n")
		}
	}
	return "", src // unterminated. Let the compiler complain
}

func assemble(directive, code string) string {
	// A snippet that carries its own `package` clause is a complete Go
	// file (docs sometimes show the whole file: package, imports, decls).
	// It is emitted verbatim: the directive contributes nothing, because
	// its imports and decls would have to merge inside the snippet's own
	// declarations. A `package main` file with no main function (the doc
	// shows only the interesting declarations) gets an empty main so the
	// package links.
	if packageClause.MatchString(code) {
		if strings.Contains(code, "package main") && !strings.Contains(code, "func main(") {
			return code + "\n\nfunc main() {}\n"
		}
		return code
	}
	var extraImports, decls, stmts []string
	for l := range strings.SplitSeq(directive, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
		case strings.HasPrefix(t, "import "):
			extraImports = append(extraImports, t)
		case strings.HasPrefix(t, "stmt:"):
			stmts = append(stmts, strings.TrimSpace(strings.TrimPrefix(t, "stmt:")))
		default:
			decls = append(decls, l)
		}
	}
	imports, rest := splitImports(code)

	var b strings.Builder
	b.WriteString("package main\n\n")
	if imports != "" {
		b.WriteString(imports + "\n\n")
	}
	for _, i := range extraImports {
		b.WriteString(i + "\n")
	}
	b.WriteString("\n")
	for _, d := range decls {
		b.WriteString(d + "\n")
	}

	// A snippet that declares its own functions is emitted at top level;
	// statement-shaped snippets get wrapped in main. Wrapping a snippet that
	// already declares func main would nest it.
	if topLevelFunc.MatchString(rest) {
		b.WriteString("\n" + rest + "\n")
		if !strings.Contains(rest, "func main(") {
			b.WriteString("\nfunc main() {\n")
			for _, s := range stmts {
				b.WriteString("\t" + s + "\n")
			}
			b.WriteString("}\n")
		}
		return b.String()
	}

	b.WriteString("\nfunc main() {\n")
	b.WriteString(rest + "\n")
	for _, s := range stmts {
		b.WriteString("\t" + s + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// topLevelFunc detects a snippet that declares its own functions.
var topLevelFunc = regexp.MustCompile(`(?m)^func\s`)

// repoGoDirective reads the `go` line out of the repository's own go.mod so a
// generated temp module can declare the same floor.
func repoGoDirective(t *testing.T, repoRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatalf("read repo go.mod: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	t.Fatalf("no go directive in %s/go.mod", repoRoot)
	return ""
}

func TestDocExamplesCompile(t *testing.T) {
	entries, err := fs.ReadDir(contentFS, "content")
	if err != nil {
		t.Fatalf("read content dir: %v", err)
	}

	var snippets []snippet
	totalFences := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := fs.ReadFile(contentFS, "content/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		text := string(body)
		totalFences += len(goFence.FindAllString(text, -1))
		for _, m := range compileDirective.FindAllStringSubmatch(text, -1) {
			snippets = append(snippets, snippet{
				doc:  e.Name(),
				body: assemble(m[1], m[2]),
			})
		}
	}

	// Report coverage rather than letting an opt-in gate read as full coverage.
	t.Logf("compiling %d of %d fenced go blocks (opt in with a gofastr:compile directive)",
		len(snippets), totalFences)
	if len(snippets) == 0 {
		t.Fatal("no gofastr:compile directives found — the gate would pass vacuously")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	dir := t.TempDir()
	// Track the repo's own `go` directive rather than hardcoding one: this
	// module `replace`s gofastr to the checkout, and Go refuses to build a
	// module whose directive is below a dependency's, so a hardcoded version
	// breaks this gate on every toolchain bump.
	gomod := fmt.Sprintf(`module docsnippet

go %s

require github.com/DonaldMurillo/gofastr v0.0.0

replace github.com/DonaldMurillo/gofastr => %s
`, repoGoDirective(t, root), root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	index := map[string]snippet{}
	for i, s := range snippets {
		pkg := fmt.Sprintf("s%02d", i)
		if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", pkg, err)
		}
		if err := os.WriteFile(filepath.Join(dir, pkg, "main.go"), []byte(s.body), 0o644); err != nil {
			t.Fatalf("write %s: %v", pkg, err)
		}
		index[pkg] = s
	}

	// Binaries go to their own directory: `go build ./...` would otherwise try
	// to write an executable named after each package dir, onto that dir.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binDir+string(os.PathSeparator), "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}

	// Map each failing package back to the doc that produced it.
	t.Errorf("doc examples failed to compile:\n%s", out)
	for pkg, s := range index {
		if strings.Contains(string(out), pkg) {
			t.Errorf("\n--- %s (generated from %s) ---\n%s", pkg, s.doc, s.body)
		}
	}
}
