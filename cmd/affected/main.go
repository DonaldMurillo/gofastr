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
//     and XTestImports, whose own transitive deps the first rule covers);
//   - a changed file matches a pattern one of the package's test files
//     declares it reads (see "Files a test reads" below).
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
// Files a test reads: a gate that walks the repo (every _test.go, every
// package.json, every exported symbol) depends on files no import edge
// reaches. internal/chromedptest's launch-allowance gate reads every test
// file in the module, so a new hand-booted Chrome in another package
// never selected it, and #536's missing allowance reached main. Such a
// test names its inputs with a directive at the start of a comment line
// in the test file:
//
//	//gofastr:test-reads *_test.go
//	//gofastr:test-reads framework/ui/ README.md core-ui/**/*.js
//
// A pattern ending in "/" is a directory prefix; one with no "/" matches
// the base name anywhere ("*_test.go", "package.json"); any other is a
// repo-relative glob where "**" spans directories. "importers:<import
// path>" matches a change in any package that imports that path
// directly, for a gate that reads the source of every caller of an API
// (core-ui/runtime's registry gates). Patterns are space-separated and a
// file may carry several directive lines. A match selects the declaring
// package only: nothing imports a test.
//
// Usage:
//
//	go run ./cmd/affected                         # one import path per line
//	go run ./cmd/affected -format dir             # ./relative/dir per line
//	go run ./cmd/affected -- go test -short       # run with the set appended
//	go run ./cmd/affected -tags red -format dir
//	go run ./cmd/affected -exclude 'examples/site$' -- go test -p 2
//	go run ./cmd/affected -match '/framework(/|$)' -- go test
//	go run ./cmd/affected -coverage-dir           # where wrapped commands keep coverage manifests
//
// With a command after `--`, an empty set skips the command and exits 0
// with a note on stderr: `go test` with no packages would test the
// current directory, which is never what a caller meant. The command's
// own exit code is propagated otherwise.
//
// The affected set is the first half of what nx affected does; Go's test
// cache is the second. A package in the set whose binary and recorded
// inputs (files read, env vars) match a previous pass replays "(cached)"
// instead of running. That only holds while tests read nothing the suite
// itself rewrites, so the wrapped command gets GOFASTR_AXE_COVERAGE_DIR
// pointed outside the module (see commandEnv), and CI fails a second run
// over the same tree that re-runs any package
// (scripts/test-cache-replay.sh).
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
	"crypto/sha256"
	"encoding/hex"
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
	Imports      []string
	Deps         []string
	TestImports  []string
	XTestImports []string
	TestGoFiles  []string
	XTestGoFiles []string

	reads testReadSet // from //gofastr:test-reads in the test files
}

// testReadSet is what a package's tests declare they read: repo-relative
// path patterns, and import paths whose direct importers they read.
type testReadSet struct {
	paths       []*regexp.Regexp
	importersOf []string
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
		covDir   = flag.Bool("coverage-dir", false, "print the coverage-manifest directory a wrapped command gets, then exit")
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
	if *covDir {
		fmt.Println(coverageDir(opts.root))
		return
	}

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
	c.Env = commandEnv(opts.root)
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fatal(err)
	}
}

// coverageDirEnv moves the axe and semantic-coverage manifests (semcov
// falls back to it) out of the module.
const coverageDirEnv = "GOFASTR_AXE_COVERAGE_DIR"

// commandEnv is the wrapped command's environment. Test binaries merge
// what they scanned into one manifest per coverage root, by default
// .gofastr/ at the module root, and read it back: a strict-mode boot
// reads the axe manifest, every recorder reads before it merges. That
// file changes during every sweep, and Go's test cache hashes the
// in-module files a test opens, so every package touching it re-ran on
// an unchanged tree. Outside the module Go does not recheck it. The
// directory is fixed per checkout, because the variable's value is a
// cache input too, and survives runs, because a package replayed from
// the cache records nothing. An explicit setting wins.
//
// It also turns off VCS stamping for every go build under it. A stamped
// build runs git, which takes and drops index.lock in .git; that moves the
// .git directory's mtime, the root listing that every repo-walking test
// hashes carries it, and those tests re-ran on the next sweep. Tests do
// not read the stamp.
func commandEnv(root string) []string {
	env := append(os.Environ(), "GOFLAGS="+noVCSStamp(os.Getenv("GOFLAGS")))
	if os.Getenv(coverageDirEnv) != "" {
		return env
	}
	if dir := coverageDir(root); dir != "" {
		env = append(env, coverageDirEnv+"="+dir)
	}
	return env
}

// noVCSStamp adds -buildvcs=false to goflags unless it already sets
// -buildvcs either way.
func noVCSStamp(goflags string) string {
	for _, f := range strings.Fields(goflags) {
		name, _, _ := strings.Cut(strings.TrimLeft(f, "-"), "=")
		if name == "buildvcs" {
			return goflags
		}
	}
	return strings.TrimSpace(goflags + " -buildvcs=false")
}

// coverageDir is the coverage root commandEnv hands the wrapped command:
// the explicit setting, else a per-checkout directory in the user cache,
// else "" (the module root) when there is no user cache, which it says on
// stderr because every test that records coverage then misses the cache.
// -coverage-dir prints it, so `gofastr verify` in the hooks reads the
// manifests the sweep wrote.
func coverageDir(root string) string {
	if v := os.Getenv(coverageDirEnv); v != "" {
		return v
	}
	base, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "affected: no user cache directory (%v); coverage manifests stay in the module and tests that record them will not replay from the cache\n", err)
		return ""
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(base, "gofastr", "test-coverage", hex.EncodeToString(sum[:6]))
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

	owners := ownerDirs(o.root, files, all)
	affected := closure(all, owners)
	for _, p := range all {
		if p.reads.matches(files, owners, all) {
			affected[p.ImportPath] = true
		}
	}
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
	args := []string{"list", "-e", "-json=ImportPath,Name,Dir,Imports,Deps,TestImports,XTestImports,TestGoFiles,XTestGoFiles"}
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
	if err := scanTestFiles(pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

// scanTestFiles reads every package's test files once for the edges go
// list cannot report: commands the tests run, and files they declare
// they read.
func scanTestFiles(pkgs []pkg) error {
	var mains []string
	for _, p := range pkgs {
		if p.Name == "main" {
			mains = append(mains, p.ImportPath)
		}
	}
	for i := range pkgs {
		p := &pkgs[i]
		for _, name := range append(slices.Clone(p.TestGoFiles), p.XTestGoFiles...) {
			path := filepath.Join(p.Dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			addCommandRuns(p, src, mains)
			if err := p.reads.parse(src); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	return nil
}

// addCommandRuns records a test that runs one of the module's commands
// as a test import of that command. A test that shells out to
// `go run <module>/cmd/gofastr` depends on the generator's source as
// much as an import would, but go list sees no edge: examples/meridian's
// generated-tree gate stayed out of the affected set when the CLI
// generator changed, and the stale tree surfaced only in CI. The edge is
// read from the full import path spelled as a Go string literal in the
// package's test files.
func addCommandRuns(p *pkg, src []byte, mains []string) {
	for _, m := range mains {
		if m != p.ImportPath && bytes.Contains(src, []byte(`"`+m+`"`)) && !slices.Contains(p.TestImports, m) {
			p.TestImports = append(p.TestImports, m)
		}
	}
}

const testReadsDirective = "//gofastr:test-reads"

// parse adds the patterns of every //gofastr:test-reads line in src. The
// directive must open the line's comment, so prose that mentions it
// declares nothing. A directive with no pattern is an error: it would
// read as coverage while selecting nothing.
func (r *testReadSet) parse(src []byte) error {
	for _, line := range strings.Split(string(src), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), testReadsDirective)
		if !ok || rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return fmt.Errorf("%s names no pattern", testReadsDirective)
		}
		for _, f := range fields {
			if imp, ok := strings.CutPrefix(f, "importers:"); ok {
				if imp == "" {
					return fmt.Errorf("%s: importers: names no import path", testReadsDirective)
				}
				r.importersOf = append(r.importersOf, imp)
				continue
			}
			r.paths = append(r.paths, readPattern(f))
		}
	}
	return nil
}

// readPattern turns one test-reads pattern into a regexp over
// repo-relative slash paths: "dir/" is a prefix, a pattern with no "/"
// matches the base name in any directory, and otherwise "**/" spans zero
// or more directories, "*" and "?" stay within one.
func readPattern(pat string) *regexp.Regexp {
	if strings.HasSuffix(pat, "/") {
		return regexp.MustCompile("^" + regexp.QuoteMeta(pat))
	}
	if !strings.Contains(pat, "/") {
		pat = "**/" + pat
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pat); i++ {
		switch {
		case strings.HasPrefix(pat[i:], "**/"):
			b.WriteString("(?:[^/]+/)*")
			i += 2
		case strings.HasPrefix(pat[i:], "**"):
			b.WriteString(".*")
			i++
		case pat[i] == '*':
			b.WriteString("[^/]*")
		case pat[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(pat[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// matches reports whether a changed file matches a path pattern, or a
// changed package (owners, by directory) directly imports a declared
// import path.
func (r testReadSet) matches(files []string, owners map[string]bool, all []pkg) bool {
	for _, f := range files {
		for _, re := range r.paths {
			if re.MatchString(f) {
				return true
			}
		}
	}
	if len(r.importersOf) == 0 {
		return false
	}
	for _, p := range all {
		if owners[p.Dir] && slices.ContainsFunc(r.importersOf, func(imp string) bool {
			return slices.Contains(p.Imports, imp)
		}) {
			return true
		}
	}
	return false
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
