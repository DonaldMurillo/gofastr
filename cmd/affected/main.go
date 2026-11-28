// affected prints the Go packages whose tests could change outcome given
// what differs between the working tree and a base ref, or runs the
// command after `--` with them appended. What counts as affected is the
// tools.affected primitive (go run ./cmd/primitives show tools.affected).
//
// Usage:
//
//	go run ./cmd/affected                         # one import path per line
//	go run ./cmd/affected -format dir             # ./relative/dir per line
//	go run ./cmd/affected -- go test -short       # run with the set appended
//	go run ./cmd/affected -exclude 'examples/site$' -match '/framework(/|$)'
//	go run ./cmd/affected -coverage-dir           # where wrapped commands keep coverage manifests
//	GOOS=windows affected -files-differ-from 'GOOS=linux CGO_ENABLED=1' -- go vet
//
// Environment: GOFASTR_TEST_ALL=1 (same as -all), GOFASTR_AFFECTED_BASE
// (base ref when -base is not given, else origin/main, else main),
// GOFASTR_AFFECTED_DIFF_BASE=1 (same as -diff-base).
//
// Exits 0 on success, including an empty set, 2 on infrastructure error,
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
	"go/scanner"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/coverageroot"
)

var alwaysAll = []string{"go.mod", "go.sum", "go.work", "go.work.sum", "cmd/affected/", "scripts/coverage-floors.sh"}

type options struct {
	root         string   // repository root (absolute)
	base         string   // base ref; empty means resolve the default
	diffBase     bool     // diff against base directly, not its merge base
	tags         string   // -tags value passed through to go list
	all          bool     // skip the graph, list everything
	files        []string // change set override (repo-relative); nil = ask git
	allIfChanged []string // extra prefixes that widen to everything
	differFrom   []string // KEY=VALUE environment; keep packages whose Go files differ under it
	differing    map[string]bool
	exclude      *regexp.Regexp
	match        *regexp.Regexp
	format       string // "import" or "dir"
	stderr       io.Writer
}

type pkg struct {
	ImportPath         string
	Name               string
	Dir                string
	Imports            []string
	IgnoredGoFiles     []string // build-excluded; a registry gate reads them anyway
	EmbedPatterns      []string
	TestEmbedPatterns  []string
	XTestEmbedPatterns []string
	Deps               []string
	TestImports        []string
	XTestImports       []string
	TestGoFiles        []string
	XTestGoFiles       []string
	GoFiles            []string
	CgoFiles           []string

	reads testReadSet // from //gofastr:test-reads in the test files
}

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
		differ   = flag.String("files-differ-from", "", "keep only packages whose Go files differ from those built under this space-separated KEY=VALUE environment, e.g. 'GOOS=linux CGO_ENABLED=1'")
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
	opts.differFrom = strings.Fields(*differ)
	for _, kv := range opts.differFrom {
		if k, _, ok := strings.Cut(kv, "="); !ok || k == "" {
			fatal(fmt.Errorf("-files-differ-from wants KEY=VALUE pairs, got %q", kv))
		}
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

	pkgs, changed, err := compute(opts)
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
	c.Env = changeEnv(commandEnv(opts.root), changed, os.Stderr)
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fatal(err)
	}
}

func commandEnv(root string) []string {
	env := append(os.Environ(), "GOFLAGS="+noVCSStamp(os.Getenv("GOFLAGS")))
	if os.Getenv(coverageroot.AxeDirEnv) != "" {
		return env
	}
	if dir := coverageDir(root); dir != "" {
		env = append(env, coverageroot.AxeDirEnv+"="+dir)
	}
	return env
}

const changedFilesEnv = "GOFASTR_AFFECTED_FILES"

const maxChangedEnv = 64 << 10

func changeEnv(env, changed []string, stderr io.Writer) []string {
	if changed == nil {
		return append(env, "GOFASTR_TEST_ALL=1")
	}
	list := strings.Join(changed, "\n")
	if len(list) > maxChangedEnv || slices.ContainsFunc(changed, func(path string) bool { return strings.ContainsAny(path, "\r\n") }) {
		fmt.Fprintf(stderr, "affected: %d changed files are too many or contain line breaks for %s; it runs as GOFASTR_TEST_ALL=1\n", len(changed), changedFilesEnv)
		return append(env, "GOFASTR_TEST_ALL=1")
	}
	return append(env, changedFilesEnv+"="+list)
}

func noVCSStamp(goflags string) string {
	for _, f := range strings.Fields(goflags) {
		name, _, _ := strings.Cut(strings.TrimLeft(f, "-"), "=")
		if name == "buildvcs" {
			return goflags
		}
	}
	return strings.TrimSpace(goflags + " -buildvcs=false")
}

func coverageDir(root string) string {
	if v := os.Getenv(coverageroot.AxeDirEnv); v != "" {
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

func compute(o options) ([]string, []string, error) {
	all, err := listPackages(o.root, o.tags)
	if err != nil {
		return nil, nil, err
	}
	if len(o.differFrom) > 0 {
		if o.differing, err = filesDiffer(o, all); err != nil {
			return nil, nil, err
		}
	}
	if o.all {
		return format(o, all, nil), nil, nil
	}

	files, base := o.files, ""
	if files == nil {
		files, base, err = changedFiles(o)
		if err != nil {
			fmt.Fprintf(o.stderr, "affected: %v; testing everything\n", err)
			return format(o, all, nil), nil, nil
		}
	}
	if trigger := widensToAll(files, o.allIfChanged); trigger != "" {
		fmt.Fprintf(o.stderr, "affected: %s changed; testing everything\n", trigger)
		return format(o, all, nil), nil, nil
	}

	owners := ownerDirs(o.root, files, all)
	affected := closure(all, owners, dirImportPaths(o.root, files, all))
	named := namedImports(o.root, base, files, owners, all)
	for _, p := range all {
		if p.reads.matches(files, owners, all, named) {
			affected[p.ImportPath] = true
		}
	}
	return format(o, all, affected), files, nil
}

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
		abs := filepath.Join(root, filepath.FromSlash(f))
		for _, p := range all {
			if embeds(p, abs) {
				owners[p.Dir] = true
			}
		}
		d := filepath.Dir(abs)
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

func dirImportPaths(root string, files []string, all []pkg) []string {
	module := ""
	if len(all) > 0 {
		rel, _ := filepath.Rel(root, all[0].Dir)
		// A root package's path stays whole: "/." is never its suffix.
		module, _ = strings.CutSuffix(all[0].ImportPath, "/"+filepath.ToSlash(rel))
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, path.Join(module, path.Dir(f)))
	}
	return paths
}

func embeds(p pkg, file string) bool {
	rel, err := filepath.Rel(p.Dir, file)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	for _, pat := range slices.Concat(p.EmbedPatterns, p.TestEmbedPatterns, p.XTestEmbedPatterns) {
		pat = strings.TrimPrefix(pat, "all:")
		for at := rel; at != "."; at = path.Dir(at) {
			if ok, _ := path.Match(pat, at); ok {
				return true
			}
		}
	}
	return false
}

func closure(all []pkg, dirs map[string]bool, extra []string) map[string]bool {
	changed := map[string]bool{}
	for _, p := range all {
		if dirs[p.Dir] {
			changed[p.ImportPath] = true
		}
	}
	for _, e := range extra {
		changed[e] = true
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
	viaTests := map[string]bool{}
	for _, p := range all {
		if hit(p.TestImports, affected) || hit(p.XTestImports, affected) ||
			hit(p.TestImports, changed) || hit(p.XTestImports, changed) {
			viaTests[p.ImportPath] = true
		}
	}
	for k := range viaTests {
		affected[k] = true
	}
	return affected
}

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
		if o.differing != nil && !o.differing[p.ImportPath] {
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

func goFiles(p pkg) string {
	lists := [][]string{p.GoFiles, p.CgoFiles, p.TestGoFiles, p.XTestGoFiles}
	parts := make([]string, len(lists))
	for i, l := range lists {
		parts[i] = strings.Join(l, "\x00")
	}
	return strings.Join(parts, "\x01")
}

func filesDiffer(o options, here []pkg) (map[string]bool, error) {
	args := []string{"list", "-e", "-json=ImportPath,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles"}
	if o.tags != "" {
		args = append(args, "-tags", o.tags)
	}
	cmd := exec.Command("go", append(args, "./...")...)
	cmd.Dir = o.root
	cmd.Env = append(os.Environ(), o.differFrom...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list under %s: %w\n%s", strings.Join(o.differFrom, " "), err, stderr.String())
	}
	there := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list output: %w", err)
		}
		there[p.ImportPath] = goFiles(p)
	}
	differing := map[string]bool{}
	for _, p := range here {
		if files, ok := there[p.ImportPath]; !ok || files != goFiles(p) {
			differing[p.ImportPath] = true
		}
	}
	return differing, nil
}

func listPackages(root, tags string) ([]pkg, error) {
	args := []string{"list", "-e", "-json=ImportPath,Name,Dir,Imports,IgnoredGoFiles,EmbedPatterns,TestEmbedPatterns,XTestEmbedPatterns,Deps,TestImports,XTestImports,TestGoFiles,XTestGoFiles,GoFiles,CgoFiles"}
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

func addCommandRuns(p *pkg, src []byte, mains []string) {
	for _, m := range mains {
		if m != p.ImportPath && bytes.Contains(src, []byte(`"`+m+`"`)) && !slices.Contains(p.TestImports, m) {
			p.TestImports = append(p.TestImports, m)
		}
	}
}

const testReadsDirective = "//gofastr:test-reads"

func (r *testReadSet) parse(src []byte) error {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	sc.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, scanner.ScanComments)
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		rest, ok := strings.CutPrefix(lit, testReadsDirective)
		if !ok {
			continue
		}
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			return fmt.Errorf("%s needs a space before its patterns: %q", testReadsDirective, lit)
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
			re, err := readPattern(f)
			if err != nil {
				return fmt.Errorf("%s: %w", testReadsDirective, err)
			}
			r.paths = append(r.paths, re)
		}
	}
	return nil
}

func readPattern(pat string) (*regexp.Regexp, error) {
	for segment := range strings.SplitSeq(strings.Trim(pat, "/"), "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("pattern %q: paths are repo-relative, without %q", pat, segment)
		}
	}
	if strings.Contains(pat, "//") {
		return nil, fmt.Errorf("pattern %q contains an empty path segment", pat)
	}
	switch {
	case strings.HasPrefix(pat, "/"):
		pat = strings.TrimPrefix(pat, "/")
		if pat == "" || strings.HasPrefix(pat, "/") {
			return nil, fmt.Errorf("pattern %q names no path", "/"+pat)
		}
	case !strings.Contains(pat, "/"):
		pat = "**/" + pat
	}
	if strings.HasSuffix(pat, "/") {
		pat += "**"
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
	return regexp.Compile(b.String())
}

func (r testReadSet) matches(files []string, owners map[string]bool, all []pkg, named map[string]bool) bool {
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
	if slices.ContainsFunc(r.importersOf, func(imp string) bool { return named[imp] }) {
		return true
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

func namedImports(root, base string, files []string, owners map[string]bool, all []pkg) map[string]bool {
	var wanted []string
	for _, p := range all {
		wanted = append(wanted, p.reads.importersOf...)
	}
	slices.Sort(wanted)
	wanted = slices.Compact(wanted)
	var goFiles []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") {
			goFiles = append(goFiles, f)
		}
	}
	for _, p := range all {
		if !owners[p.Dir] {
			continue
		}
		for _, name := range p.IgnoredGoFiles {
			if rel, err := filepath.Rel(root, filepath.Join(p.Dir, name)); err == nil {
				goFiles = append(goFiles, filepath.ToSlash(rel))
			}
		}
	}
	named := map[string]bool{}
	if len(wanted) == 0 || len(goFiles) == 0 {
		return named
	}
	for _, imp := range wanted {
		quoted := []byte(strconv.Quote(imp))
		for _, f := range goFiles {
			if src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f))); err == nil && bytes.Contains(src, quoted) {
				named[imp] = true
				break
			}
		}
		if named[imp] || base == "" {
			continue
		}
		// git grep exits 1 on no match; only output decides.
		out, _ := gitOutput(root, append([]string{"grep", "-l", "-F", "-e", string(quoted), base, "--"}, goFiles...)...)
		if strings.TrimSpace(out) != "" {
			named[imp] = true
		}
	}
	return named
}

func changedFiles(o options) ([]string, string, error) {
	base, err := resolveBase(o)
	if err != nil {
		return nil, "", err
	}
	diff, err := gitOutput(o.root, "diff", "--name-only", "-z", "--no-renames", base)
	if err != nil {
		return nil, "", fmt.Errorf("git diff against %s: %w", base, err)
	}
	untracked, err := gitOutput(o.root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, "", fmt.Errorf("git ls-files: %w", err)
	}
	files := strings.FieldsFunc(diff+untracked, func(char rune) bool { return char == '\x00' })
	sort.Strings(files)
	return slices.Compact(files), base, nil
}

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
