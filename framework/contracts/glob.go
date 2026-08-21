package contracts

import "strings"

// matchGlob reports whether a slash-separated path matches a pattern.
// The dialect is the one people already expect from .gitignore and
// tsconfig, which is not what path.Match implements:
//
//   - any run of characters within one segment
//     **    any number of segments, including none
//     ?     one character within a segment
//     dir/  everything under dir (a trailing slash means "this subtree")
//
// A pattern with no metacharacters and no slash matches any path segment,
// so `exempt: [testdata]` does the obvious thing.
//
// One deliberate departure from gitignore: `core/**` also matches `core`
// itself, because `**` matches zero segments. gitignore reads that
// pattern as "the contents of core, but not core". Here the patterns
// mostly name layers and subtrees: `packages: ["core/**"]` means "the
// core tree", and a reader would be surprised to find the root `core`
// package outside its own layer.
func matchGlob(pattern, path string) bool {
	// Backslashes in a pattern are separators a Windows user typed. Every
	// path this matcher sees is slash-separated (Pass normalises on
	// discovery), so a backslash could otherwise only ever fail to match,
	// and it would fail *silently*, leaving an exemption that looks
	// applied and does nothing.
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "./")
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	if pattern == "" || path == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	if !strings.ContainsAny(pattern, "*?/") {
		for _, seg := range strings.Split(path, "/") {
			if seg == pattern {
				return true
			}
		}
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// matchSegments is the recursive `**`-aware walk. `**` tries every split
// point; everything else must line up one segment at a time.
func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Trailing `**` swallows the rest, including nothing.
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

// matchSegment is `*`/`?` matching inside a single path segment.
func matchSegment(pat, seg string) bool {
	// Iterative backtracking rather than recursion: segments are short,
	// but a pathological pattern of many `*` should still be linear-ish.
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

// MatchPath is [matchGlob] exported for analyzers that need the same
// dialect on something other than a file path, import paths, most
// obviously, which are slash-separated in exactly the same way.
func MatchPath(pattern, path string) bool { return matchGlob(pattern, path) }

// matchAnyGlob reports whether path matches any pattern in the list.
func matchAnyGlob(patterns []string, path string) bool {
	for _, p := range patterns {
		if matchGlob(p, path) {
			return true
		}
	}
	return false
}
