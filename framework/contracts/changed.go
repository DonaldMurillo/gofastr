package contracts

import (
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ChangedFiles returns the root-relative paths that differ from ref, plus
// untracked files, as a set for [Report.RestrictTo]. ref may be a branch
// or commit, compared from its merge base, or "" for the working tree
// against HEAD. Outside a git repository it returns (nil, nil).
func ChangedFiles(root, ref string) (map[string]bool, error) {
	if !insideGitRepo(root) {
		return nil, nil
	}
	args := []string{"diff", "--name-only", "-z", "--diff-filter=ACMR"}
	if ref == "" {
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

	rel, err := repoRelativePrefix(root)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	add := func(line, prefix string) {
		p := filepath.ToSlash(line)
		if p == "" {
			return
		}
		if prefix != "" {
			if !strings.HasPrefix(p, prefix) {
				return
			}
			p = strings.TrimPrefix(p, prefix)
		}
		if p != "" {
			changed[p] = true
		}
	}
	for _, line := range strings.Split(out, "\x00") {
		add(line, rel)
	}
	untracked, err := gitOutput(root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(untracked, "\x00") {
		add(line, "")
	}
	return changed, nil
}

func mergeBase(root, ref string) (string, error) {
	base, err := gitOutput(root, "merge-base", "HEAD", ref)
	if err != nil {
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

// RestrictTo drops every diagnostic outside the given file set, those
// with no file included, records the count in OutsideChange and returns
// it. A nil set is a no-op.
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
	return slices.Sorted(maps.Keys(files))
}

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
