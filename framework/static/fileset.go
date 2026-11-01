package static

import (
	"fmt"
	"maps"
	"slices"
	"strings"
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

// caseTwinError refuses two spellings of one file. A case-insensitive
// filesystem keeps whichever spelling was written first and a
// case-sensitive host serves only that one, so a page asking for the
// other gets a 404.
func caseTwinError(a, b string) error {
	return fmt.Errorf("static: %s and %s differ only in case and are one file on a case-insensitive filesystem; give one of them another name", a, b)
}

// refuseSeparatorName refuses an fs.FS name holding a backslash, which
// fs.ValidPath allows as a literal character but Windows reads as a
// separator: about\index.html would land on about/index.html there,
// past every check keyed on the slash path. A NUL names no file.
func refuseSeparatorName(name string) error {
	if strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("static: file %q holds a backslash or NUL; name it with / only", name)
	}
	return nil
}

// checkCaseTwins fails the build when two files it wrote differ only in
// case: a contributor's _HEADERS rewritten later as _headers kept the
// contributor's spelling on APFS, and a case-sensitive host then had no
// _headers. Every page, asset, doc and host-rule file is compared.
func checkCaseTwins(res *Result, rendered *fileSet, docs map[string]bool) error {
	seen := map[string]string{}
	check := func(rel string) error {
		f := foldPath(rel)
		if prev, ok := seen[f]; ok && prev != rel {
			return caseTwinError("/"+prev, "/"+rel)
		}
		seen[f] = rel
		return nil
	}
	for _, p := range rendered.paths() {
		if err := check(p); err != nil {
			return err
		}
	}
	for _, p := range slices.Sorted(maps.Keys(docs)) {
		if err := check(p); err != nil {
			return err
		}
	}
	for _, a := range res.Assets {
		if err := check(strings.TrimPrefix(a, "/")); err != nil {
			return err
		}
	}
	return nil
}
