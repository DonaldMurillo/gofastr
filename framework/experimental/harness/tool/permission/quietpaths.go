package permission

import (
	"os"
	"path/filepath"
	"strings"
)

var quietPathCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "wc": true,
	"grep": true, "rg": true, "find": true, "ls": true, "git": true,
}

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
			return true
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

func bashPathsInside(base, baseReal, cmd string) bool {
	words, ok := splitWords(cmd)
	if !ok || len(words) == 0 {
		return false
	}
	if !quietPathCommands[words[0]] {
		return true
	}
	for _, w := range words[1:] {
		switch {
		case strings.HasPrefix(w, "--"):
			if _, v, ok := strings.Cut(w, "="); ok && !shellPathInside(base, baseReal, v) {
				return false
			}
		case strings.HasPrefix(w, "-") && len(w) > 1:
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
