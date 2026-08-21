package contracts

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ChangedFiles returns the repository-relative paths that differ from
// ref, as a set suitable for [Report.RestrictTo].
//
// Why this exists: verify is all-or-nothing otherwise. A pre-commit hook,
// a dev-loop rebuild, and a PR review all want the same narrower
// question, "what did *this change* break", and answering it by reading
// a 200-line whole-repo report is answering a different question.
//
// The analysis itself still runs over the whole tree, and must: the route
// table, the entity list, and the coverage manifest are only meaningful
// whole. Only the *reporting* narrows. A duplicate route introduced by
// editing one file is still found, because the other half of the pair was
// analysed too.
//
// ref may be a branch, a commit, or "" for the working tree against HEAD.
// A repository-less directory returns (nil, nil), not an error, because
// "not in git" is a legitimate state and the caller should simply not
// narrow.
func ChangedFiles(root, ref string) (map[string]bool, error) {
	if !insideGitRepo(root) {
		return nil, nil
	}
	args := []string{"diff", "--name-only", "--diff-filter=ACMR"}
	if ref == "" {
		// Working tree plus index against HEAD: what is uncommitted.
		args = append(args, "HEAD")
	} else {
		base, err := mergeBase(root, ref)
		if err != nil {
			return nil, err
		}
		args = append(args, base)
	}
	out, err := gitOutput(root, args...)
	if err != nil {
		return nil, err
	}

	// `git diff --name-only` reports paths relative to the REPOSITORY
	// root, while diagnostics are relative to the analysed root. When the
	// two differ, an app inside a monorepo, `--root examples/site`, or
	// the dev watcher in any subdirectory, every tracked path failed to
	// match and RestrictTo dropped it as "outside the change". The file
	// just edited was the one silently withheld, while untracked files
	// (which git reports relative to the cwd) still matched, so the run
	// looked like it was working.
	rel, err := repoRelativePrefix(root)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	add := func(line, prefix string) {
		p := filepath.ToSlash(strings.TrimSpace(line))
		if p == "" {
			return
		}
		if prefix != "" {
			// Paths outside the analysed root are not this run's concern.
			if !strings.HasPrefix(p, prefix) {
				return
			}
			p = strings.TrimPrefix(p, prefix)
		}
		if p != "" {
			changed[p] = true
		}
	}
	for _, line := range strings.Split(out, "\n") {
		add(line, rel)
	}
	// Untracked files are part of "what I changed" too: a brand new file
	// full of findings is exactly what a pre-commit check should catch,
	// and `git diff` never lists it.
	if ref == "" {
		untracked, err := gitOutput(root, "ls-files", "--others", "--exclude-standard")
		if err == nil {
			// `ls-files` runs with cwd set to root, so its output is
			// already root-relative, no prefix to strip.
			for _, line := range strings.Split(untracked, "\n") {
				add(line, "")
			}
		}
	}
	return changed, nil
}

// mergeBase resolves the fork point with ref, so a long-lived branch is
// compared against where it diverged rather than against the tip, which
// would otherwise report every finding in everyone else's commits as
// something this branch changed.
func mergeBase(root, ref string) (string, error) {
	base, err := gitOutput(root, "merge-base", "HEAD", ref)
	if err != nil {
		// No common ancestor (an unrelated history, a fresh shallow
		// clone). Comparing against the ref directly is the honest
		// fallback, and it is what the user literally asked for.
		return ref, nil
	}
	if b := strings.TrimSpace(base); b != "" {
		return b, nil
	}
	return ref, nil
}

func insideGitRepo(root string) bool {
	out, err := gitOutput(root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// RestrictTo drops every diagnostic outside the given file set and
// returns how many it removed.
//
// Diagnostics with no file, the ones about the project as a whole, like
// a missing coverage manifest, are dropped too. They are not about the
// change under review, and surfacing them on every narrowed run is the
// noise that makes people stop reading.
func (r *Report) RestrictTo(files map[string]bool) int {
	if files == nil {
		return 0
	}
	kept := r.Diagnostics[:0]
	dropped := 0
	for _, d := range r.Diagnostics {
		if d.File != "" && files[d.File] {
			kept = append(kept, d)
			continue
		}
		dropped++
	}
	r.Diagnostics = kept
	r.OutsideChange = dropped
	r.summarize()
	return dropped
}

// SortedFiles renders a file set deterministically, for reporting.
func SortedFiles(files map[string]bool) []string {
	out := make([]string, 0, len(files))
	for f := range files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// repoRelativePrefix is root's path within its repository, slash-separated
// and trailing-slashed, or "" when root IS the repository root. It is the
// prefix to strip from repo-relative git output to reach the paths the
// analysed tree uses.
func repoRelativePrefix(root string) (string, error) {
	top, err := gitOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	top = strings.TrimSpace(top)
	if top == "" {
		return "", nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// Resolve symlinks on both sides: on macOS /tmp is a symlink to
	// /private/tmp, and git reports the resolved form while filepath.Abs
	// does not, leaving a prefix that never matches.
	if resolved, linkErr := filepath.EvalSymlinks(abs); linkErr == nil {
		abs = resolved
	}
	if resolved, linkErr := filepath.EvalSymlinks(top); linkErr == nil {
		top = resolved
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return "", nil
	}
	return rel + "/", nil
}
