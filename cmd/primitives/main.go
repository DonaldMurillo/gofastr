// primitives reads the primitives registry under primitives/ (format:
// primitives/README.md).
//
//	primitives                  list the top-level primitives
//	primitives for <path>...    what governs these files: owner chain, invariants, decisions
//	primitives show <id>        one primitive in full, with its children
//	primitives lint             check every reference in the registry against the tree
//	primitives gen [--check]    write the generated files, or fail when they are stale
//	primitives judge [--base <ref>] [--json]
//	                            list the judged questions that apply to the changed files
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

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
	case "judge":
		return judge(c, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "primitives: unknown command %q (list, for, show, lint, gen, judge)\n", cmd)
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

func printFor(c *primitives.Registry, w io.Writer, file string) {
	chain := c.Chain(file)
	if len(chain) == 0 {
		fmt.Fprintf(w, "%s: no primitive owns this path yet\n", file)
		return
	}
	fmt.Fprintf(w, "%s\n", file)
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
