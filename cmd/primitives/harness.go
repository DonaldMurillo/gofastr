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

type hookEvent struct {
	Event   string `json:"hook_event_name"`
	Session string `json:"session_id"`
	Active  bool   `json:"stop_hook_active"`
}

func readHookEvent(stdin io.Reader) (hookEvent, error) {
	var event hookEvent
	//gofastr:allow(GOFASTR1407) not a request body: the event comes from the Claude Code process that started this hook, with no second parser to disagree with, and it carries keys this command does not read, which the strict decoder refuses
	err := json.NewDecoder(stdin).Decode(&event)
	return event, err
}

// hook answers a Claude Code PostToolUse event with the same-job notice
// for each changed file that is a twins site the session was not yet told about.
func hook(c *primitives.Registry, stdin io.Reader, stdout io.Writer) int {
	event, err := readHookEvent(stdin)
	if err != nil {
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

var stopBudget = 5 * time.Minute

// stop answers a Claude Code Stop event, printing a block decision when
// stopCheck has a reason to send the agent back.
func stop(c *primitives.Registry, loadFindings []primitives.Finding, stdin io.Reader, stdout, stderr io.Writer) int {
	reason, warning := stopCheck(c, loadFindings, stdin)
	if warning != "" {
		fmt.Fprintln(stderr, warning)
	}
	if reason == "" {
		return 0
	}
	return writeStopBlock(stdout, reason)
}

func stopUnreadable(loadErr error, stdin io.Reader, stdout io.Writer) int {
	if event, err := readHookEvent(stdin); err != nil || event.Active {
		return 0
	}
	reason := stopBlock([]string{fmt.Sprintf("the primitives registry cannot be read: %v", loadErr)})
	return writeStopBlock(stdout, reason)
}

func writeStopBlock(stdout io.Writer, reason string) int {
	if err := json.NewEncoder(stdout).Encode(map[string]string{"decision": "block", "reason": reason}); err != nil {
		return 2
	}
	return 0
}

func stopBlock(problems []string) string {
	return "What you changed does not pass yet. Fix it before you finish.\n\n" + strings.Join(problems, "\n")
}

// stopCheck returns why the session may not finish yet, or a warning when
// it could not tell.
func stopCheck(c *primitives.Registry, loadFindings []primitives.Finding, stdin io.Reader) (reason, warning string) {
	event, err := readHookEvent(stdin)
	if err != nil || event.Active {
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
	groups := changedPackageGroups(c.Root, changed)
	modules := changedModules(c.Root, changed)
	registryChanged := slices.ContainsFunc(changed, func(file string) bool { return strings.HasPrefix(file, primitives.Dir+"/") })
	governed := slices.ContainsFunc(changed, func(file string) bool {
		return len(c.AppliedTo(file)) > 0 || slices.ContainsFunc(c.Chain(file), func(p *primitives.Primitive) bool { return len(p.OnChange) > 0 })
	})
	if len(groups) == 0 && len(modules) == 0 && !registryChanged && !governed {
		return "", ""
	}
	state := ""
	if commit, err := exec.Command("git", "-C", c.Root, "rev-parse", "--verify", "--quiet", base).Output(); err == nil {
		state = treeState(c.Root, strings.TrimSpace(string(commit)), changed)
	}
	tested := state != "" && slices.Contains(toldFiles(c.Root, "stop-pass"), state)
	var notices []docsNotice
	if sessionName.MatchString(event.Session) {
		notices = docsNotices(c, changed, toldFiles(c.Root, docsKey(event.Session)))
	}
	var problems []string
	for _, f := range append(loadFindings, c.Lint()...) {
		problems = append(problems, f.String())
	}
	if stale, err := c.Stale(); err != nil {
		problems = append(problems, fmt.Sprintf("generated files could not be checked: %v", err))
	} else {
		for _, rel := range slices.Sorted(maps.Keys(stale)) {
			problems = append(problems, fmt.Sprintf("%s is %s: run `go run ./cmd/primitives gen`", rel, stale[rel]))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopBudget)
	defer cancel()
	unfinished := func(warning string) (string, string) {
		if len(problems) == 0 {
			return "", warning
		}
		return stopBlock(problems), warning
	}
	checks := [][]string{{"vet"}, {"test", "-short", "-count=1"}}
	if tested {
		checks, modules = nil, nil
	}
	for _, dir := range modules {
		cmd := exec.CommandContext(ctx, "go", "build", "./...")
		cmd.Dir = dir
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return unfinished(fmt.Sprintf("primitives stop: go build did not finish in %s in %s; run it yourself before you call this done.", stopBudget, dir))
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("go build ./... failed in %s:\n%s", dir, failures(string(out), 40)))
		}
	}
	for _, args := range checks {
		failed := false
		for _, g := range groups {
			cmd := exec.CommandContext(ctx, "go", append(append([]string{}, args...), g.pkgs...)...)
			cmd.Dir = g.dir
			cmd.WaitDelay = 10 * time.Second
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				return unfinished(fmt.Sprintf("primitives stop: go %s did not finish in %s for %s; run it yourself before you call this done.", args[0], stopBudget, strings.Join(g.pkgs, " ")))
			}
			if err != nil {
				problems = append(problems, fmt.Sprintf("go %s failed:\n%s", strings.Join(args, " "), failures(string(out), 40)))
				failed = true
				break
			}
		}
		if failed {
			break
		}
	}
	if len(problems) > 0 {
		return stopBlock(problems), ""
	}
	if state != "" {
		rememberTold(c.Root, "stop-pass", []string{state})
	}
	var lines []string
	if sessionName.MatchString(event.Session) && len(toldFiles(c.Root, siblingsKey(event.Session))) == 0 {
		rememberTold(c.Root, siblingsKey(event.Session), []string{"asked"})
		lines = append(lines, siblingsQuestion)
	}
	if len(notices) > 0 {
		var ids []string
		for _, n := range notices {
			ids = append(ids, n.primitive)
			lines = append(lines, n.text)
		}
		rememberTold(c.Root, docsKey(event.Session), append(toldFiles(c.Root, docsKey(event.Session)), ids...))
	}
	if len(lines) > 0 {
		return "Before you finish:\n\n" + strings.Join(lines, "\n\n"), ""
	}
	return "", ""
}

const siblingsQuestion = "Does any other code path do the same job as the one you changed: a sibling handler, another mode or spelling of the same input, the layer above or below? " +
	"Check them for the same defect or the same missing change. Fix what you find, or list in your summary the paths you checked and why each needs nothing."

func siblingsKey(session string) string { return session + "-siblings" }

func docsKey(session string) string { return session + "-docs" }

type docsNotice struct{ primitive, text string }

func docsNotices(c *primitives.Registry, changed, told []string) []docsNotice {
	var out []docsNotice
	seen := map[string]bool{}
	applied := map[string]int{}
	appliedFiles := map[string][]string{}
	appliedRules := map[string]primitives.Invariant{}
	for _, file := range changed {
		for _, a := range c.AppliedTo(file) {
			key := a.Primitive.ID + "/" + a.Invariant.ID
			if slices.Contains(told, key) {
				continue
			}
			if _, ok := applied[key]; ok {
				appliedFiles[key] = append(appliedFiles[key], file)
				continue
			}
			applied[key] = len(out)
			appliedFiles[key] = []string{file}
			out = append(out, docsNotice{primitive: key})
			appliedRules[key] = a.Invariant
		}
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		goFile := strings.HasSuffix(file, ".go")
		for _, p := range c.Chain(file) {
			if seen[p.ID] || slices.Contains(told, p.ID) || (len(p.Docs) == 0 && len(p.OnChange) == 0) {
				continue
			}
			if !goFile && len(p.OnChange) == 0 {
				continue
			}
			seen[p.ID] = true
			var docs []string
			touched := false
			for _, ref := range p.Docs {
				docs = append(docs, ref.Value)
				touched = touched || slices.Contains(changed, ref.Value)
			}
			if (touched || len(docs) == 0) && len(p.OnChange) == 0 {
				continue
			}
			var b strings.Builder
			fmt.Fprintf(&b, "You changed %s (%s)", p.ID, strings.TrimSuffix(p.Summary, "."))
			if len(docs) > 0 && !touched {
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
	for _, key := range slices.Sorted(maps.Keys(applied)) {
		inv := appliedRules[key]
		text := fmt.Sprintf("You changed %s, which %s also governs: %s", strings.Join(appliedFiles[key], ", "), key, inv.Rule)
		if inv.Judge != "" {
			text += "\n  Ask of your change: " + inv.Judge
		}
		out[applied[key]].text = text
	}
	return out
}

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

// packageGroup is one go vet or go test invocation: the module directory
// to run in, and the package patterns relative to it.
type packageGroup struct {
	dir  string
	pkgs []string
}

func testdataPath(rel string) bool {
	return strings.Contains("/"+path.Clean(rel)+"/", "/testdata/")
}

func changedModules(root string, changed []string) []string {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var dirs []string
	for _, file := range changed {
		if base := path.Base(file); base != "go.mod" && base != "go.sum" {
			continue
		}
		rel := path.Dir(file)
		if testdataPath(rel) {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := tree.Stat(path.Join(rel, "go.mod")); err == nil && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func changedPackageGroups(root string, changed []string) []packageGroup {
	seen := map[string][]string{}
	var order []string
	for _, file := range changed {
		dir := path.Dir(file)
		if !strings.HasSuffix(file, ".go") || testdataPath(dir) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(dir))
		if matches, _ := filepath.Glob(filepath.Join(abs, "*.go")); len(matches) == 0 {
			continue
		}
		mod := enclosingModule(root, abs)
		rel, err := filepath.Rel(mod, abs)
		if err != nil {
			continue
		}
		pkg := "."
		if rel != "." {
			pkg = "./" + filepath.ToSlash(rel)
		}
		if _, ok := seen[mod]; !ok {
			order = append(order, mod)
		}
		if !slices.Contains(seen[mod], pkg) {
			seen[mod] = append(seen[mod], pkg)
		}
	}
	out := make([]packageGroup, 0, len(order))
	for _, dir := range order {
		out = append(out, packageGroup{dir: dir, pkgs: seen[dir]})
	}
	return out
}

func enclosingModule(root, dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if dir == root {
			return root
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return root
		}
		dir = parent
	}
}

// treeState hashes the base commit and the changed files' content; it is
// empty, meaning "test", when a file cannot be read.
func treeState(root, baseCommit string, changed []string) string {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer dir.Close()
	sum := sha256.New()
	fmt.Fprintf(sum, "base %s\n", baseCommit)
	for _, file := range changed {
		data, err := dir.ReadFile(file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fmt.Fprintf(sum, "%s deleted\n", file)
			continue
		case err != nil:
			return ""
		}
		fmt.Fprintf(sum, "%s present %d\n", file, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

var failureLine = regexp.MustCompile(`^(--- FAIL|FAIL|panic:|\S+\.go:\d+:\d*:? |\s+\S+_test\.go:\d+: )`)

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
