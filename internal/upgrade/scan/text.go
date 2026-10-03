package scan

import (
	"path"
	"strings"
)

// textFile is the last resort: a per-line regex over files no other
// matcher reads. Go and CSS files are never text targets — their
// matchers read them structurally, and the file dispatch never routes
// them here.
func (e *engine) textFile(rel string, read func() ([]byte, bool)) {
	if len(e.textWants) == 0 || strings.HasSuffix(rel, ".go") {
		return
	}
	var wants []textWant
	for _, w := range e.textWants {
		if matchGlob(w.tm.Glob, rel) {
			wants = append(wants, w)
		}
	}
	if len(wants) == 0 {
		return
	}
	src, ok := read()
	if !ok {
		return
	}
	for i, line := range strings.Split(string(src), "\n") {
		for _, w := range wants {
			if w.tm.Match.MatchString(line) {
				e.add(w.n, Hit{File: rel, Line: i + 1, Col: 1, Why: "text " + w.tm.Match.String()})
			}
		}
	}
}

// matchGlob matches a root-relative slash path against a glob where
// "**" crosses directories and "*" stays within one segment.
func matchGlob(pattern, name string) bool {
	return globSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func globSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := range len(name) + 1 {
			if globSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
		return false
	}
	return globSegments(pat[1:], name[1:])
}
