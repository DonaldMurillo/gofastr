package permission

import (
	"os"
	"path/filepath"
	"strings"
)

// Quiet mode reads only under the working directory. The preset was
// documented as "Read, Glob, Ls (anywhere under the repo)", `cat
// <repo-file>` and `find . *`, but it allowed any path: Read:/proc/self/
// environ, ~/.ssh/id_rsa, `cat /proc/self/environ`, `grep -r .
// ~/.config/gofastr` and `find / -name creds.enc` all ran with no prompt.
// quietPathsInside is the path half of the preset: every path a call
// names must resolve, symlinks included, under the working directory,
// and a path it cannot place (~, .., an expansion it does not perform)
// asks instead.

// quietPathCommands are the allow-listed Bash commands whose arguments
// name files to read. Their every non-flag argument (and every flag
// value) is checked as a path; a pattern argument such as grep's is
// checked too, which only ever makes the check stricter.
var quietPathCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "wc": true,
	"grep": true, "rg": true, "find": true, "ls": true, "git": true,
}

// quietPathsInside reports whether every path the call reads lies under
// base. toolName and argv are what Evaluate was given.
func quietPathsInside(base, toolName, argv string) bool {
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return false
		}
		base = wd
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	switch toolName {
	case "Read":
		p, ok := strings.CutPrefix(argv, "Read:")
		return ok && pathInside(base, baseReal, p)
	case "Ls":
		if argv == "Ls" {
			return true // no path: the working directory
		}
		p, ok := strings.CutPrefix(argv, "Ls:")
		return ok && pathInside(base, baseReal, p)
	case "Glob":
		rest, ok := strings.CutPrefix(argv, "Glob:")
		if !ok {
			return false
		}
		words, ok := splitWords(rest)
		if !ok || len(words) == 0 {
			return false
		}
		pattern, flags := words[0], words[1:]
		root := ""
		for i := 0; i < len(flags); i += 2 {
			if i+1 >= len(flags) || flags[i] != "--root" {
				return false
			}
			root = flags[i+1]
		}
		if root == "" {
			root = "."
		}
		return pathInside(base, baseReal, root) && toolGlobInside(base, baseReal, root, pattern)
	case "Grep":
		rest, ok := strings.CutPrefix(argv, "Grep:")
		if !ok {
			return false
		}
		words, ok := splitWords(rest)
		if !ok || len(words) == 0 {
			return false
		}
		path, glob := ".", ""
		flags := words[1:]
		for i := 0; i < len(flags); i += 2 {
			if i+1 >= len(flags) {
				return false
			}
			switch flags[i] {
			case "--path":
				path = flags[i+1]
			case "--glob":
				glob = flags[i+1]
			default:
				return false
			}
		}
		if !pathInside(base, baseReal, path) {
			return false
		}
		return glob == "" || toolGlobInside(base, baseReal, path, glob)
	case "Bash":
		return bashPathsInside(base, baseReal, argv)
	}
	return false
}

// bashPathsInside checks the path arguments of an allow-listed read
// command. bashQuietAllow has already refused every shell
// metacharacter, so no expansion other than ~ and filename globs can
// reach the command.
func bashPathsInside(base, baseReal, cmd string) bool {
	words, ok := splitWords(cmd)
	if !ok || len(words) == 0 {
		return false
	}
	if !quietPathCommands[words[0]] {
		return true // pwd, echo: read no file
	}
	for _, w := range words[1:] {
		switch {
		case strings.HasPrefix(w, "--"):
			if _, v, ok := strings.Cut(w, "="); ok && !shellPathInside(base, baseReal, v) {
				return false
			}
		case strings.HasPrefix(w, "-") && len(w) > 1:
			// A short flag with its value attached (grep -f/etc/shadow).
			if strings.ContainsAny(w, "/~") && !shellPathInside(base, baseReal, w[2:]) {
				return false
			}
		default:
			if !shellPathInside(base, baseReal, w) {
				return false
			}
		}
	}
	return true
}

// shellPathInside checks one shell word: a glob must match only paths
// under base, the way the shell would expand it there.
func shellPathInside(base, baseReal, w string) bool {
	if !pathInside(base, baseReal, w) {
		return false
	}
	if !strings.ContainsAny(w, "*?[") {
		return true
	}
	pattern := w
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(base, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false
	}
	for _, m := range matches {
		if !pathInside(base, baseReal, m) {
			return false
		}
	}
	return true
}

// toolGlobInside checks a Glob tool pattern (or Grep's --glob) read
// under root: relative, with no .. and no ~, and its fixed leading
// directories resolving under base.
func toolGlobInside(base, baseReal, root, pattern string) bool {
	if pattern == "" || filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") {
		return false
	}
	if !pathInside(base, baseReal, pattern) {
		return false
	}
	var fixed []string
	for _, seg := range strings.Split(filepath.ToSlash(pattern), "/") {
		if strings.ContainsAny(seg, "*?[{") {
			break
		}
		fixed = append(fixed, seg)
	}
	return pathInside(base, baseReal, filepath.Join(append([]string{root}, fixed...)...))
}

// pathInside reports whether p, relative to base or absolute, resolves
// (symlinks followed as far as the path exists) under baseReal. A path
// starting with ~, holding a .. segment, or naming a variable asks.
func pathInside(base, baseReal, p string) bool {
	if p == "" || strings.HasPrefix(p, "~") || strings.ContainsAny(p, "$`\x00") {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return false
		}
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(base, full)
	}
	real, ok := resolveExisting(filepath.Clean(full))
	if !ok {
		return false
	}
	rel, err := filepath.Rel(baseReal, real)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// resolveExisting resolves the symlinks of the longest existing prefix
// of an absolute, clean path and appends the rest unchanged: a file not
// created yet still has a parent that decides where it would be.
func resolveExisting(p string) (string, bool) {
	var rest []string
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(append([]string{real}, rest...)...), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// splitWords splits s into words the way a POSIX shell does for the
// quoting this package sees: whitespace separates, single quotes keep
// everything, double quotes keep everything but a backslash escape, and
// a backslash outside quotes keeps the next byte. ok is false on an
// unterminated quote.
func splitWords(s string) ([]string, bool) {
	var (
		words []string
		cur   strings.Builder
		in    bool
	)
	flush := func() {
		if in {
			words = append(words, cur.String())
			cur.Reset()
			in = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '\'':
			in = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			in = true
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, false
			}
		case c == '\\':
			in = true
			if i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			}
		default:
			in = true
			cur.WriteByte(c)
		}
	}
	flush()
	return words, true
}
