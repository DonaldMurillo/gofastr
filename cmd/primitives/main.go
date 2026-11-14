// primitives reads the primitives registry under primitives/ (format:
// primitives/README.md).
//
//	primitives                  list the top-level primitives
//	primitives for <path>...    what governs these files: owner chain, invariants, decisions
//	primitives show <id>        one primitive in full, with its children
//	primitives lint             check every reference in the registry against the tree
//	primitives gen [--check]    write the generated files, or fail when they are stale
//	primitives hook             a Claude Code PostToolUse hook: tells the agent, once per
//	                            session, what else does the job of a file it changed
//	primitives stop             a Claude Code Stop hook: sends the agent back while the
//	                            packages it changed fail vet or their tests
//	primitives judge [--base <ref>] [--json]
//	                            list the judged questions that apply to the changed files
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/primitives"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	os.Exit(run(root, os.Args[1:], os.Stdout, os.Stderr))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "primitives: %v\n", err)
	os.Exit(2)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, primitives.Dir)); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s/ directory beside a go.mod above the working directory", primitives.Dir)
		}
		dir = parent
	}
}

func run(root string, args []string, stdout, stderr io.Writer) int {
	c, findings, err := primitives.Load(root)
	if err != nil {
		fmt.Fprintf(stderr, "primitives: %v\n", err)
		return 2
	}
	cmd := "list"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd != "lint" && len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(stderr, f)
		}
		fmt.Fprintln(stderr, "primitives: the registry does not parse; fix the findings above")
		return 1
	}
	switch cmd {
	case "list":
		for _, p := range c.Children("") {
			fmt.Fprintf(stdout, "%s: %s\n", p.ID, p.Summary)
		}
		fmt.Fprintln(stdout, "\nNext: primitives show <id>, or primitives for <path>")
	case "for":
		if len(args) == 0 {
			fmt.Fprintln(stderr, "usage: primitives for <path>...")
			return 2
		}
		for i, arg := range args {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			printFor(c, stdout, relPath(root, arg))
		}
	case "show":
		if len(args) != 1 || c.Primitive(args[0]) == nil {
			fmt.Fprintln(stderr, "usage: primitives show <id>   (primitives lists the top-level ids)")
			return 2
		}
		printPrimitive(c, stdout, c.Primitive(args[0]), true)
	case "lint":
		findings = append(findings, c.Lint()...)
		for _, f := range findings {
			fmt.Fprintln(stderr, f)
		}
		if len(findings) > 0 {
			fmt.Fprintf(stderr, "primitives: %d finding(s)\n", len(findings))
			return 1
		}
		invariants, judged, decisions := 0, 0, 0
		for _, p := range c.Primitives {
			decisions += len(p.Decisions)
			for _, inv := range p.Invariants {
				invariants++
				if len(inv.EnforcedBy) == 0 {
					judged++
				}
			}
		}
		fmt.Fprintf(stdout, "primitives: %d primitives, %d invariants (%d judged only), %d decisions: clean\n",
			len(c.Primitives), invariants, judged, decisions)
	case "gen":
		return gen(c, args, stdout, stderr)
	case "hook":
		return hook(c, os.Stdin, stdout)
	case "stop":
		return stop(c, os.Stdin, stdout, stderr)
	case "judge":
		return judge(c, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "primitives: unknown command %q (list, for, show, lint, gen, hook, stop, judge)\n", cmd)
		return 2
	}
	return 0
}

func relPath(root, arg string) string {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return filepath.ToSlash(arg)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(arg)
	}
	return filepath.ToSlash(rel)
}

// hook answers a Claude Code PostToolUse event. It looks at what the
// working tree has changed, by any tool, and the first time a changed
// file is a site of a twins entry it returns the same-job notice as
// context for the agent. On input it cannot read it says nothing.
func hook(c *primitives.Registry, stdin io.Reader, stdout io.Writer) int {
	var event struct {
		Event   string `json:"hook_event_name"`
		Session string `json:"session_id"`
	}
	if err := json.NewDecoder(stdin).Decode(&event); err != nil {
		return 0
	}
	changed, err := changedFiles(c.Root, "HEAD")
	if err != nil {
		return 0
	}
	told := toldFiles(c.Root, event.Session)
	var b strings.Builder
	var fresh []string
	for _, file := range changed {
		twins := c.TwinsOf(file)
		if len(twins) == 0 || slices.Contains(told, file) {
			continue
		}
		fresh = append(fresh, file)
		fmt.Fprintf(&b, "You changed %s, and the primitives registry has a note on it.\n", file)
		printTwins(&b, twins)
	}
	if len(fresh) == 0 {
		return 0
	}
	rememberTold(c.Root, event.Session, append(told, fresh...))
	out := map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event.Event, "additionalContext": b.String()}}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return 2
	}
	return 0
}

// stopBudget is how long the Stop hook may spend on vet and tests before
// it gives up and lets the session finish with a warning.
const stopBudget = 5 * time.Minute

// stop answers a Claude Code Stop event. When stopCheck has something to
// say it prints a block decision, which sends the agent back with the
// reason. The decision goes to stdout with exit 0 because the hook runs
// through `go run`, which reports every failing exit code as 1, and
// Claude Code only treats 2 as blocking.
func stop(c *primitives.Registry, stdin io.Reader, stdout, stderr io.Writer) int {
	reason, warning := stopCheck(c, stdin)
	if warning != "" {
		fmt.Fprintln(stderr, warning)
	}
	if reason == "" {
		return 0
	}
	if err := json.NewEncoder(stdout).Encode(map[string]string{"decision": "block", "reason": reason}); err != nil {
		return 2
	}
	return 0
}

// stopCheck checks the registry and runs vet and the short tests of
// every package changed since the base ref, and returns why the session
// may not finish yet, or a warning when it could not tell. A session the
// hook has already sent back once is let go, so a failure it cannot fix
// does not hold it forever.
func stopCheck(c *primitives.Registry, stdin io.Reader) (reason, warning string) {
	var event struct {
		Active  bool   `json:"stop_hook_active"`
		Session string `json:"session_id"`
	}
	if err := json.NewDecoder(stdin).Decode(&event); err != nil || event.Active {
		return "", ""
	}
	base := "origin/main"
	if err := exec.Command("git", "-C", c.Root, "rev-parse", "--verify", "--quiet", base).Run(); err != nil {
		base = "HEAD"
	}
	changed, err := changedFiles(c.Root, base)
	if err != nil {
		return "", ""
	}
	changed = slices.DeleteFunc(changed, func(file string) bool { return commentOnly(c.Root, base, file) })
	pkgs := changedPackages(c.Root, changed)
	if len(pkgs) == 0 {
		return "", ""
	}
	state := treeState(c.Root, changed)
	tested := state != "" && slices.Contains(toldFiles(c.Root, "stop-pass"), state)
	notices := docsNotices(c, changed, toldFiles(c.Root, docsKey(event.Session)))
	var problems []string
	for _, f := range c.Lint() {
		problems = append(problems, f.String())
	}
	if stale, err := c.Stale(); err == nil {
		for _, rel := range slices.Sorted(maps.Keys(stale)) {
			problems = append(problems, fmt.Sprintf("%s is %s: run `go run ./cmd/primitives gen`", rel, stale[rel]))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopBudget)
	defer cancel()
	checks := [][]string{{"vet"}, {"test", "-short", "-count=1"}}
	if tested {
		checks = nil
	}
	for _, args := range checks {
		cmd := exec.CommandContext(ctx, "go", append(args, pkgs...)...)
		cmd.Dir = c.Root
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return "", fmt.Sprintf("primitives stop: go %s did not finish in %s for %s; run it yourself before you call this done.", args[0], stopBudget, strings.Join(pkgs, " "))
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("go %s failed:\n%s", strings.Join(args, " "), failures(string(out), 40)))
			break
		}
	}
	if len(problems) > 0 {
		return "What you changed does not pass yet. Fix it before you finish.\n\n" + strings.Join(problems, "\n"), ""
	}
	if state != "" {
		rememberTold(c.Root, "stop-pass", []string{state})
	}
	if len(notices) > 0 {
		var ids, lines []string
		for _, n := range notices {
			ids = append(ids, n.primitive)
			lines = append(lines, n.text)
		}
		rememberTold(c.Root, docsKey(event.Session), append(toldFiles(c.Root, docsKey(event.Session)), ids...))
		return "Before you finish:\n\n" + strings.Join(lines, "\n\n"), ""
	}
	return "", ""
}

func docsKey(session string) string { return session + "-docs" }

type docsNotice struct{ primitive, text string }

// docsNotices names, for each primitive whose code changed while none of
// its docs did, the docs to update and anything else the primitive asks
// for on a change. A primitive the session was already told about is
// left out.
func docsNotices(c *primitives.Registry, changed, told []string) []docsNotice {
	var out []docsNotice
	seen := map[string]bool{}
	for _, file := range changed {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		for _, p := range c.Chain(file) {
			if seen[p.ID] || slices.Contains(told, p.ID) || (len(p.Docs) == 0 && len(p.OnChange) == 0) {
				continue
			}
			seen[p.ID] = true
			var docs []string
			touched := false
			for _, ref := range p.Docs {
				docs = append(docs, ref.Value)
				touched = touched || slices.Contains(changed, ref.Value)
			}
			if touched {
				continue
			}
			var b strings.Builder
			fmt.Fprintf(&b, "You changed %s (%s)", p.ID, strings.TrimSuffix(p.Summary, "."))
			if len(docs) > 0 {
				fmt.Fprintf(&b, ", and none of its docs changed: %s. Update them, or say in your summary why this change needs none.", strings.Join(docs, ", "))
			} else {
				b.WriteString(".")
			}
			for _, ref := range p.OnChange {
				fmt.Fprintf(&b, "\n  Also, for this part: %s", ref.Value)
			}
			out = append(out, docsNotice{p.ID, b.String()})
		}
	}
	return out
}

// commentOnly reports whether a Go file differs from its base version in
// comments alone. Such an edit cannot change what the code does, so it
// needs no test run and no docs. Directive comments (//go:, //gofastr:,
// // +build) count as code. A file that is new, gone or unreadable is
// not comment-only.
func commentOnly(root, base, file string) bool {
	if !strings.HasSuffix(file, ".go") {
		return false
	}
	before, err := exec.Command("git", "-C", root, "show", base+":"+file).Output()
	if err != nil {
		return false
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer dir.Close()
	after, err := dir.ReadFile(file)
	if err != nil {
		return false
	}
	a, okA := codeTokens(before)
	b, okB := codeTokens(after)
	return okA && okB && slices.Equal(a, b)
}

// codeTokens is a Go file's token stream without its ordinary comments.
func codeTokens(src []byte) ([]string, bool) {
	var s scanner.Scanner
	fset := token.NewFileSet()
	ok := true
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, func(token.Position, string) { ok = false }, scanner.ScanComments)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out, ok
		}
		if tok == token.COMMENT {
			if !strings.HasPrefix(lit, "//go:") && !strings.HasPrefix(lit, "//gofastr:") && !strings.HasPrefix(lit, "// +build") {
				continue
			}
		}
		if tok == token.SEMICOLON && lit == "\n" {
			lit = ";"
		}
		out = append(out, tok.String()+" "+lit)
	}
}

// changedPackages lists, as ./dir patterns, the directories that hold a
// changed Go file and still hold Go code.
func changedPackages(root string, changed []string) []string {
	var pkgs []string
	for _, file := range changed {
		dir := path.Dir(file)
		if !strings.HasSuffix(file, ".go") || strings.Contains("/"+dir+"/", "/testdata/") {
			continue
		}
		pattern := "./" + dir
		if slices.Contains(pkgs, pattern) {
			continue
		}
		if matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.go")); len(matches) > 0 {
			pkgs = append(pkgs, pattern)
		}
	}
	return pkgs
}

// treeState names the content of the changed files, so a tree that
// already passed is not tested again. It is empty when a file cannot be
// read, which means "test".
func treeState(root string, changed []string) string {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer dir.Close()
	sum := sha256.New()
	for _, file := range changed {
		data, err := dir.ReadFile(file)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		fmt.Fprintf(sum, "%s %d\n", file, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

var failureLine = regexp.MustCompile(`^(--- FAIL|FAIL|panic:|\S+\.go:\d+:\d*:? |\s+\S+_test\.go:\d+: )`)

// failures keeps the lines of a go vet or go test run that say what
// failed: the failing tests, their messages and the failing packages. A
// run that names none falls back to its last lines.
func failures(out string, n int) string {
	var kept []string
	for line := range strings.SplitSeq(out, "\n") {
		if failureLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return tail(out, n)
	}
	if len(kept) > n {
		kept = append(kept[:n], fmt.Sprintf("... and %d more lines", len(kept)-n))
	}
	return strings.Join(kept, "\n")
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

const hookStateDir = ".gofastr/primitives-hook"

var sessionName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// toldFiles lists the files this session has already been told about.
// A session the hook cannot name is told every time.
func toldFiles(root, session string) []string {
	if !sessionName.MatchString(session) {
		return nil
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer dir.Close()
	data, err := dir.ReadFile(hookStateDir + "/" + session)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func rememberTold(root, session string, files []string) {
	if !sessionName.MatchString(session) {
		return
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer dir.Close()
	if err := dir.MkdirAll(hookStateDir, 0o700); err != nil {
		return
	}
	state, err := dir.OpenFile(hookStateDir+"/"+session, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer state.Close()
	if _, err := state.WriteString(strings.Join(files, "\n") + "\n"); err != nil {
		return
	}
}

func printTwins(w io.Writer, twins []primitives.Twin) {
	for _, twin := range twins {
		fmt.Fprintf(w, "\nSAME JOB IN SEVERAL PLACES: %s\n", twin.Job)
		for _, site := range twin.Sites {
			fmt.Fprintf(w, "  %s: %s\n", site.Path, site.Note)
		}
		fmt.Fprintln(w, "  In this repository a fix to one of these is not finished until every site has it. Fix them all, or say why a site is exempt.")
	}
}

func printFor(c *primitives.Registry, w io.Writer, file string) {
	chain := c.Chain(file)
	twins := c.TwinsOf(file)
	if len(chain) == 0 && len(twins) == 0 {
		fmt.Fprintf(w, "%s: no primitive owns this path yet\n", file)
		return
	}
	fmt.Fprintf(w, "%s\n", file)
	printTwins(w, twins)
	if len(chain) == 0 {
		return
	}
	for _, p := range chain {
		printPrimitive(c, w, p, false)
	}
	fmt.Fprintf(w, "Next: primitives show %s (rejected options and revisit conditions)\n", chain[len(chain)-1].ID)
}

func printPrimitive(c *primitives.Registry, w io.Writer, p *primitives.Primitive, full bool) {
	fmt.Fprintf(w, "\n%s: %s\n", p.ID, p.Summary)
	if full {
		for _, ref := range p.Code {
			fmt.Fprintf(w, "  code: %s\n", ref.Value)
		}
		for _, ref := range p.Docs {
			fmt.Fprintf(w, "  docs: %s\n", ref.Value)
		}
	}
	for _, ref := range p.OnChange {
		fmt.Fprintf(w, "  on change: %s\n", ref.Value)
	}
	for _, rule := range p.Imports {
		fmt.Fprintf(w, "  must not link %s. %s Instead: %s\n", rule.Deny, rule.Why, rule.Instead)
	}
	for _, inv := range p.Invariants {
		fmt.Fprintf(w, "  invariant %s: %s\n", inv.ID, inv.Rule)
		for _, e := range inv.EnforcedBy {
			fmt.Fprintf(w, "    %s %s\n", e.Kind, e.Ref)
		}
		if inv.Judge != "" {
			fmt.Fprintf(w, "    judge: %s\n", inv.Judge)
		}
	}
	for _, d := range p.Decisions {
		status := ""
		if d.Status == primitives.StatusSuperseded {
			status = " (superseded by " + d.SupersededBy + ")"
		}
		fmt.Fprintf(w, "  decision %s%s: %s\n", d.ID, status, d.Decision)
		if !full {
			continue
		}
		for _, r := range d.Rejected {
			fmt.Fprintf(w, "    rejected %s: %s\n", strings.TrimSuffix(r.Option, "."), r.Why)
		}
		if d.RevisitIf != "" {
			fmt.Fprintf(w, "    revisit if: %s\n", d.RevisitIf)
		}
	}
	if full {
		for _, child := range c.Children(p.ID) {
			fmt.Fprintf(w, "  child %s: %s\n", child.ID, child.Summary)
		}
	}
}

func gen(c *primitives.Registry, args []string, stdout, stderr io.Writer) int {
	check := len(args) == 1 && args[0] == "--check"
	if len(args) > 0 && !check {
		fmt.Fprintln(stderr, "usage: primitives gen [--check]")
		return 2
	}
	stale, err := c.Stale()
	if err != nil {
		fmt.Fprintf(stderr, "primitives: %v\n", err)
		return 2
	}
	if check {
		for _, rel := range slices.Sorted(maps.Keys(stale)) {
			fmt.Fprintf(stderr, "%s: %s\n", rel, stale[rel])
		}
		if len(stale) > 0 {
			fmt.Fprintln(stderr, "primitives: generated files are stale; run `go run ./cmd/primitives gen`")
			return 1
		}
		return 0
	}
	files, err := c.Generate()
	if err != nil {
		fmt.Fprintf(stderr, "primitives: %v\n", err)
		return 2
	}
	dir, err := os.OpenRoot(c.Root)
	if err != nil {
		fmt.Fprintf(stderr, "primitives: %v\n", err)
		return 2
	}
	defer dir.Close()
	for _, rel := range slices.Sorted(maps.Keys(stale)) {
		if stale[rel] == "orphaned" {
			err = dir.Remove(rel)
		} else {
			//gofastr:allow(worldreadable) a generated Go test file, committed to the repository like every other source file.
			err = dir.WriteFile(rel, files[rel], 0o644)
		}
		if err != nil {
			fmt.Fprintf(stderr, "primitives: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "%s: %s, fixed\n", rel, stale[rel])
	}
	return 0
}

func judge(c *primitives.Registry, args []string, stdout, stderr io.Writer) int {
	base, asJSON := "origin/main", false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			asJSON = true
		case args[i] == "--base" && i+1 < len(args):
			i++
			base = args[i]
		default:
			fmt.Fprintln(stderr, "usage: primitives judge [--base <ref>] [--json]")
			return 2
		}
	}
	changed, err := changedFiles(c.Root, base)
	if err != nil {
		fmt.Fprintf(stderr, "primitives: %v\n", err)
		return 2
	}
	checks := c.Checks(changed)
	if asJSON {
		if checks == nil {
			checks = []primitives.Check{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(checks); err != nil {
			fmt.Fprintf(stderr, "primitives: %v\n", err)
			return 2
		}
		return 0
	}
	for _, check := range checks {
		fmt.Fprintf(stdout, "%s/%s\n  rule: %s\n  ask:  %s\n  in:   %s\n", check.Primitive, check.Invariant, check.Rule, check.Question, strings.Join(check.Files, ", "))
	}
	fmt.Fprintf(stdout, "primitives: %d judged question(s) apply to %d changed file(s) against %s\n", len(checks), len(changed), base)
	return 0
}

func changedFiles(root, base string) ([]string, error) {
	seen := map[string]bool{}
	for _, args := range [][]string{
		{"diff", "--name-only", "-z", base},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		for name := range strings.SplitSeq(string(out), "\x00") {
			if name != "" {
				seen[name] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}
