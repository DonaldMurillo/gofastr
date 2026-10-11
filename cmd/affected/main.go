// affected prints the Go packages whose test outcome could change given
// what differs between the working tree and a base ref, the way
// `nx affected` scopes a JavaScript monorepo. It is the default scope for
// `make test`, the git hooks, the red and coverage sweeps, and CI's
// pull-request run; `scripts/test-all.sh` and GOFASTR_TEST_ALL=1 are the
// explicit "test everything" spellings.
//
// A package is affected when any of these hold:
//
//   - a changed file sits in the package's directory or anywhere below it
//     that is not itself a package (testdata, embedded assets, fixtures);
//   - the package imports an affected package, transitively (go list's
//     Deps);
//   - the package's test files import an affected package (TestImports
//     and XTestImports, whose own transitive deps the first rule covers).
//
// Some changes make the graph meaningless and widen the set to every
// package: go.mod, go.sum, go.work, and this tool's own source. A base ref
// that cannot be resolved, or a merge base that cannot be computed, also
// widens to everything, so a misconfigured checkout degrades to the full
// run and never to a skipped one. -all-if-changed adds callers' own
// triggers (the vet step passes internal/analyzers: a new rule can flag
// code nobody touched).
//
// The change set is the working tree against the merge base of HEAD and
// the base ref: staged, unstaged and untracked files all count, and a
// deleted file still marks the package it left. With -diff-base the base
// ref is diffed directly instead; CI uses that with the pull request's
// base SHA, where the checkout is the PR's merge commit and no history is
// fetched.
//
// Usage:
//
//	go run ./cmd/affected                         # one import path per line
//	go run ./cmd/affected -format dir             # ./relative/dir per line
//	go run ./cmd/affected -- go test -short       # run with the set appended
//	go run ./cmd/affected -tags red -format dir
//	go run ./cmd/affected -exclude 'examples/site$' -- go test -p 2
//	go run ./cmd/affected -match '/framework(/|$)' -- go test
//
// With a command after `--`, an empty set skips the command and exits 0
// with a note on stderr: `go test` with no packages would test the
// current directory, which is never what a caller meant. The command's
// own exit code is propagated otherwise.
//
// Environment:
//
//	GOFASTR_TEST_ALL=1             every package, no graph walk (same as -all)
//	GOFASTR_AFFECTED_BASE          base ref when -base is not given
//	GOFASTR_AFFECTED_DIFF_BASE=1   same as -diff-base
//
// Without a base, origin/main is used when that ref exists, then main.
//
// Exits 0 on success (including an empty set), 2 on infrastructure error,
// or with the wrapped command's status.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// alwaysAll lists repo-relative paths whose change widens the set to every
// package. Directory entries match everything beneath them.
var alwaysAll = []string{"go.mod", "go.sum", "go.work", "go.work.sum", "cmd/affected/"}

type options struct {
	root         string   // repository root (absolute)
	base         string   // base ref; empty means resolve the default
	diffBase     bool     // diff against base directly, not its merge base
	tags         string   // -tags value passed through to go list
	all          bool     // skip the graph, list everything
	files        []string // change set override (repo-relative); nil = ask git
	allIfChanged []string // extra prefixes that widen to everything
	exclude      *regexp.Regexp
	match        *regexp.Regexp
	format       string // "import" or "dir"
	stderr       io.Writer
}

type pkg struct {
	ImportPath   string
	Name         string
	Dir          string
	Deps         []string
	TestImports  []string
	XTestImports []string
	TestGoFiles  []string
	XTestGoFiles []string
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var (
		base     = flag.String("base", "", "base ref (default: $GOFASTR_AFFECTED_BASE, origin/main, main)")
		diffBase = flag.Bool("diff-base", false, "diff against -base directly instead of merge-base(HEAD, base)")
		tags     = flag.String("tags", "", "build tags handed to go list")
		all      = flag.Bool("all", false, "list every package (also $GOFASTR_TEST_ALL=1)")
		files    = flag.String("files", "", "comma-separated change set instead of asking git ('-' reads newline-separated paths from stdin)")
		exclude  = flag.String("exclude", "", "drop import paths matching this regexp from the output")
		match    = flag.String("match", "", "keep only import paths matching this regexp")
		format   = flag.String("format", "import", "output form: import (import paths) or dir (./relative dirs)")
		allIf    stringList
	)
	flag.Var(&allIf, "all-if-changed", "repo-relative file or dir/ prefix whose change widens to every package (repeatable)")
	flag.Parse()

	opts := options{
		base:         *base,
		diffBase:     *diffBase || os.Getenv("GOFASTR_AFFECTED_DIFF_BASE") == "1",
		tags:         *tags,
		all:          *all || os.Getenv("GOFASTR_TEST_ALL") == "1",
		allIfChanged: allIf,
		format:       *format,
		stderr:       os.Stderr,
	}
	if opts.base == "" {
		opts.base = os.Getenv("GOFASTR_AFFECTED_BASE")
	}
	if *exclude != "" {
		re, err := regexp.Compile(*exclude)
		if err != nil {
			fatal(fmt.Errorf("-exclude: %w", err))
		}
		opts.exclude = re
	}
	if *match != "" {
		re, err := regexp.Compile(*match)
		if err != nil {
			fatal(fmt.Errorf("-match: %w", err))
		}
		opts.match = re
	}
	switch *files {
	case "":
	case "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal(err)
		}
		opts.files = splitLines(string(b))
	default:
		opts.files = strings.Split(*files, ",")
	}
	if opts.format != "import" && opts.format != "dir" {
		fatal(fmt.Errorf("-format must be import or dir, got %q", opts.format))
	}

	root, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		fatal(fmt.Errorf("not inside a git repository: %w", err))
	}
	opts.root = strings.TrimSpace(root)

	pkgs, err := compute(opts)
	if err != nil {
		fatal(err)
	}

	cmd := flag.Args()
	if len(cmd) == 0 {
		for _, p := range pkgs {
			fmt.Println(p)
		}
		return
	}
	if len(pkgs) == 0 {
		fmt.Fprintf(os.Stderr, "affected: no packages affected, skipping: %s\n", strings.Join(cmd, " "))
		return
	}
	fmt.Fprintf(os.Stderr, "affected: %d package(s)\n", len(pkgs))
	c := exec.Command(cmd[0], append(cmd[1:], pkgs...)...) //gofastr:allow(nowaitdelay) interactive wrapper with inherited stdio; no pipes to wedge on
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "affected: %v\n", err)
	os.Exit(2)
}

// compute returns the affected package set in the requested format,
// sorted. It never returns an empty set by accident: every failure to
// establish the change set widens to all packages and says so on stderr.
func compute(o options) ([]string, error) {
	all, err := listPackages(o.root, o.tags)
	if err != nil {
		return nil, err
	}
	if o.all {
		return format(o, all, nil), nil
	}

	files := o.files
	if files == nil {
		files, err = changedFiles(o)
		if err != nil {
			fmt.Fprintf(o.stderr, "affected: %v; testing everything\n", err)
			return format(o, all, nil), nil
		}
	}
	if trigger := widensToAll(files, o.allIfChanged); trigger != "" {
		fmt.Fprintf(o.stderr, "affected: %s changed; testing everything\n", trigger)
		return format(o, all, nil), nil
	}

	affected := closure(all, ownerDirs(o.root, files, all))
	return format(o, all, affected), nil
}

// widensToAll reports the first changed path that matches an always-all
// trigger, or "" when none does.
func widensToAll(files []string, extra []string) string {
	triggers := append(slices.Clone(alwaysAll), extra...)
	for _, f := range files {
		for _, t := range triggers {
			if strings.HasSuffix(t, "/") && strings.HasPrefix(f, t) || f == t {
				return f
			}
		}
	}
	return ""
}

// ownerDirs maps every changed file to the nearest enclosing package
// directory. A file under no package (README.md, scripts/) owns nothing.
func ownerDirs(root string, files []string, all []pkg) map[string]bool {
	isPkg := make(map[string]bool, len(all))
	for _, p := range all {
		isPkg[p.Dir] = true
	}
	owners := map[string]bool{}
	for _, f := range files {
		if f == "" {
			continue
		}
		d := filepath.Dir(filepath.Join(root, filepath.FromSlash(f)))
		for {
			if isPkg[d] {
				owners[d] = true
				break
			}
			if d == root || len(d) <= len(root) {
				break
			}
			d = filepath.Dir(d)
		}
	}
	return owners
}

// closure returns the import paths affected by the packages in dirs: the
// packages themselves, their transitive importers, and every package
// whose test files import one of those.
func closure(all []pkg, dirs map[string]bool) map[string]bool {
	changed := map[string]bool{}
	for _, p := range all {
		if dirs[p.Dir] {
			changed[p.ImportPath] = true
		}
	}
	hit := func(list []string, set map[string]bool) bool {
		for _, d := range list {
			if set[d] {
				return true
			}
		}
		return false
	}
	affected := map[string]bool{}
	for _, p := range all {
		if changed[p.ImportPath] || hit(p.Deps, changed) {
			affected[p.ImportPath] = true
		}
	}
	// Test imports are direct, not transitive, so one more pass over the
	// first set is exact: a test import's own deps are regular deps,
	// already resolved above.
	viaTests := map[string]bool{}
	for _, p := range all {
		if hit(p.TestImports, affected) || hit(p.XTestImports, affected) {
			viaTests[p.ImportPath] = true
		}
	}
	for k := range viaTests {
		affected[k] = true
	}
	return affected
}

// format renders the selected packages (every package when set is nil),
// applying -exclude and -format.
func format(o options, all []pkg, set map[string]bool) []string {
	var out []string
	for _, p := range all {
		if set != nil && !set[p.ImportPath] {
			continue
		}
		if o.exclude != nil && o.exclude.MatchString(p.ImportPath) {
			continue
		}
		if o.match != nil && !o.match.MatchString(p.ImportPath) {
			continue
		}
		if o.format == "dir" {
			rel, err := filepath.Rel(o.root, p.Dir)
			if err != nil || rel == "." {
				out = append(out, ".")
				continue
			}
			out = append(out, "./"+filepath.ToSlash(rel))
			continue
		}
		out = append(out, p.ImportPath)
	}
	sort.Strings(out)
	return out
}

func listPackages(root, tags string) ([]pkg, error) {
	args := []string{"list", "-e", "-json=ImportPath,Name,Dir,Deps,TestImports,XTestImports,TestGoFiles,XTestGoFiles"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, stderr.String())
	}
	var pkgs []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	if err := addCommandRuns(pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

// addCommandRuns records a test that runs one of the module's commands
// as a test import of that command. A test that shells out to
// `go run <module>/cmd/gofastr` depends on the generator's source as
// much as an import would, but go list sees no edge: examples/meridian's
// generated-tree gate stayed out of the affected set when the CLI
// generator changed, and the stale tree surfaced only in CI. The edge is
// read from the full import path spelled as a Go string literal in the
// package's test files.
func addCommandRuns(pkgs []pkg) error {
	var mains []string
	for _, p := range pkgs {
		if p.Name == "main" {
			mains = append(mains, p.ImportPath)
		}
	}
	if len(mains) == 0 {
		return nil
	}
	for i := range pkgs {
		p := &pkgs[i]
		for _, name := range append(slices.Clone(p.TestGoFiles), p.XTestGoFiles...) {
			src, err := os.ReadFile(filepath.Join(p.Dir, name))
			if err != nil {
				return fmt.Errorf("read %s: %w", filepath.Join(p.Dir, name), err)
			}
			for _, m := range mains {
				if m != p.ImportPath && bytes.Contains(src, []byte(`"`+m+`"`)) && !slices.Contains(p.TestImports, m) {
					p.TestImports = append(p.TestImports, m)
				}
			}
		}
	}
	return nil
}

// changedFiles asks git for the change set: the working tree (staged and
// unstaged) against the base commit, plus untracked files. Renames are
// reported as a delete plus an add so the package a file left is tested
// too.
func changedFiles(o options) ([]string, error) {
	base, err := resolveBase(o)
	if err != nil {
		return nil, err
	}
	diff, err := gitOutput(o.root, "diff", "--name-only", "--no-renames", base)
	if err != nil {
		return nil, fmt.Errorf("git diff against %s: %w", base, err)
	}
	untracked, err := gitOutput(o.root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	files := append(splitLines(diff), splitLines(untracked)...)
	sort.Strings(files)
	return slices.Compact(files), nil
}

// resolveBase picks the base ref and, unless -diff-base, its merge base
// with HEAD. Any failure is an error the caller turns into "everything".
func resolveBase(o options) (string, error) {
	ref := o.base
	if ref == "" {
		for _, cand := range []string{"origin/main", "main"} {
			if _, err := gitOutput(o.root, "rev-parse", "--verify", "-q", cand+"^{commit}"); err == nil {
				ref = cand
				break
			}
		}
		if ref == "" {
			return "", errors.New("no base ref: neither origin/main nor main exists (set -base or GOFASTR_AFFECTED_BASE)")
		}
	}
	if o.diffBase {
		return ref, nil
	}
	mb, err := gitOutput(o.root, "merge-base", "HEAD", ref)
	if err != nil {
		return "", fmt.Errorf("merge-base HEAD %s: %w", ref, err)
	}
	return strings.TrimSpace(mb), nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, msg)
	}
	return string(out), nil
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
