package contracts

import "strings"

func matchGlob(pattern, path string) bool {
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "./")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	if pattern == "" || path == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	if !anchored && !strings.ContainsAny(pattern, "*?/") {
		for _, seg := range strings.Split(path, "/") {
			if seg == pattern {
				return true
			}
		}
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(path); i++ {
				if matchSegments(pat[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		if !matchSegment(pat[0], path[0]) {
			return false
		}
		pat, path = pat[1:], path[1:]
	}
	return len(path) == 0
}

func matchSegment(pat, seg string) bool {
	var pi, si, star, mark int
	star = -1
	for si < len(seg) {
		switch {
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == seg[si]):
			pi++
			si++
		case pi < len(pat) && pat[pi] == '*':
			star = pi
			mark = si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

// MatchPath reports whether a slash-separated path matches a pattern in
// the gitignore-like dialect config uses: `*` and `?` within a segment,
// `**` for any number of segments including none (so `core/**` matches
// `core`), a trailing `/` for a subtree, a leading `/` to anchor at the
// root, `\` read as `/`, and a bare name matching any one segment.
func MatchPath(pattern, path string) bool { return matchGlob(pattern, path) }

func matchAnyGlob(patterns []string, path string) bool {
	for _, p := range patterns {
		if matchGlob(p, path) {
			return true
		}
	}
	return false
}
