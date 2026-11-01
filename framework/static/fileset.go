package static

import (
	"maps"
	"slices"
)

// fileSet is a set of slash paths under OutDir that the build writes
// itself. has matches case-folded: two paths that differ only in case
// are one file on a case-insensitive filesystem (APFS, NTFS by
// default), so a copy that would land on one of these files in another
// case is refused like an exact match, and the export is the same
// whichever filesystem built it.
type fileSet struct {
	spelled map[string]bool
	folded  map[string]string // foldPath(p) -> p as written
}

func newFileSet() *fileSet {
	return &fileSet{spelled: map[string]bool{}, folded: map[string]string{}}
}

func (s *fileSet) add(rel string) {
	s.spelled[rel] = true
	s.folded[foldPath(rel)] = rel
}

// has reports whether rel, in any case, is in the set.
func (s *fileSet) has(rel string) bool {
	_, ok := s.folded[foldPath(rel)]
	return ok
}

// hasExact reports whether rel, spelled exactly, is in the set.
func (s *fileSet) hasExact(rel string) bool { return s.spelled[rel] }

// spelling returns the set's spelling of rel's file, if it has one.
func (s *fileSet) spelling(rel string) (string, bool) {
	p, ok := s.folded[foldPath(rel)]
	return p, ok
}

// paths returns the set's paths as written, sorted.
func (s *fileSet) paths() []string { return slices.Sorted(maps.Keys(s.spelled)) }
