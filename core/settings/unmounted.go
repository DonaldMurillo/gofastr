package settings

import (
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"golang.org/x/mod/module"
)

// Unmounted is one entry of the unmounted section: a gofastr package the
// project copied into its own tree with `gofastr unmount`. The section is
// the CLI's registry of copies; `gofastr unmount` and `gofastr remount`
// write it, and verify, upgrade and audit read it.
type Unmounted struct {
	// Source is the upstream import path the copy was made from.
	Source string
	// Version is the gofastr version copied: a release tag, or dev for a
	// copy made from a checkout.
	Version string
	// Path is the copy's directory, slash-separated and relative to the
	// settings file.
	Path string
	// Date is the day the copy was made, YYYY-MM-DD.
	Date string
}

var unmountedKeys = map[string]bool{"source": true, "version": true, "path": true, "date": true}

// Unmounted decodes the unmounted section, nil when the file has none.
func (f File) Unmounted() ([]Unmounted, error) {
	section := f.Section("unmounted")
	if section == nil {
		return nil, nil
	}
	if section.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s: unmounted must be a list of copies", f.Path)
	}
	var out []Unmounted
	seen := map[string]bool{}
	for i, item := range section.List {
		at := fmt.Sprintf("%s: unmounted[%d]", f.Path, i)
		if item == nil || item.Kind != coreyaml.Map {
			return nil, fmt.Errorf("%s must be a map with source, version, path and date", at)
		}
		for _, k := range slices.Sorted(maps.Keys(item.Map)) {
			if !unmountedKeys[k] {
				return nil, fmt.Errorf("%s: unknown key %q", at, k)
			}
		}
		var u Unmounted
		for key, dst := range map[string]*string{"source": &u.Source, "version": &u.Version, "path": &u.Path, "date": &u.Date} {
			n := item.Map[key]
			s, ok := "", false
			if n != nil && n.Kind == coreyaml.Scalar {
				s, ok = n.Value.(string)
			}
			if !ok {
				return nil, fmt.Errorf("%s: %s must be a string", at, key)
			}
			*dst = s
		}
		if err := u.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", at, err)
		}
		// A path holds one copy. One source may have several: `generate
		// package` copies a chrome package once per binary that owns it.
		if seen[u.Path] {
			return nil, fmt.Errorf("%s: %s is listed twice", at, u.Path)
		}
		seen[u.Path] = true
		out = append(out, u)
	}
	return out, nil
}

// Validate checks one entry: an import path, a plain version, a local
// slash path and a calendar date. Every field lands in YAML and in
// generated Go, so nothing outside those shapes is written or read.
func (u Unmounted) Validate() error {
	if err := module.CheckImportPath(u.Source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if !plainVersion(u.Version) {
		return fmt.Errorf("version %q is not a plain version", u.Version)
	}
	if u.Path == "" || u.Path != path.Clean(u.Path) || path.IsAbs(u.Path) || u.Path == "." ||
		u.Path == ".." || strings.HasPrefix(u.Path, "../") {
		return fmt.Errorf("path %q must be a clean relative path inside the project", u.Path)
	}
	if err := module.CheckImportPath("example.com/" + u.Path); err != nil {
		return fmt.Errorf("path %q cannot be part of an import path: %w", u.Path, err)
	}
	if slices.Contains(strings.Split(u.Path, "/"), "vendor") {
		return fmt.Errorf("path %q is inside a vendor directory, which the go tool will not let the app import from", u.Path)
	}
	if _, err := time.Parse(time.DateOnly, u.Date); err != nil {
		return fmt.Errorf("date %q is not YYYY-MM-DD", u.Date)
	}
	return nil
}

func plainVersion(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

// SetUnmounted writes entries as the unmounted section of the settings
// file at p, creating the file when it does not exist. The rest of the
// file keeps its bytes and comments, trailing blank lines aside; the
// unmounted section is gofastr's to write, so it is replaced whole (an
// empty list removes it, and the file with it when nothing else is left).
// The result is parsed back before it is written and must hold exactly
// entries, so a write never leaves a file the readers refuse or a section
// the rewrite failed to find.
func SetUnmounted(p string, entries []Unmounted) error {
	if info, err := os.Lstat(p); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink: gofastr writes the unmounted section into the file itself, so put the file there instead of the link", p)
	}
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) && len(entries) == 0 {
		return nil
	}
	for _, u := range entries {
		if err := u.Validate(); err != nil {
			return err
		}
	}
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b Unmounted) int {
		return cmp.Or(strings.Compare(a.Source, b.Source), strings.Compare(a.Path, b.Path))
	})

	kept := withoutSection(string(data), "unmounted")
	var b strings.Builder
	b.WriteString(kept)
	if len(entries) > 0 {
		if kept != "" && !strings.HasSuffix(kept, "\n") {
			b.WriteString("\n")
		}
		if kept != "" && !strings.HasSuffix(kept, "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString("# Written by gofastr unmount and gofastr remount.\n")
		b.WriteString("unmounted:\n")
		for _, u := range entries {
			fmt.Fprintf(&b, "  - source: %s\n    version: %s\n    path: %s\n    date: %s\n",
				strconv.Quote(u.Source), strconv.Quote(u.Version), strconv.Quote(u.Path), strconv.Quote(u.Date))
		}
	}
	out := b.String()
	if out == "" {
		// The section was all the file held: an empty file says nothing.
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	f, err := Parse(p, []byte(out))
	if err != nil {
		return err
	}
	got, err := f.Unmounted()
	if err != nil {
		return err
	}
	if !slices.Equal(got, entries) {
		return fmt.Errorf("%s: could not rewrite its unmounted section; write the key as a plain `unmounted:` at the start of a line", p)
	}
	return replaceFile(p, []byte(out))
}

// replaceFile writes data to a temporary file beside p and renames it over
// p, so a failed write leaves the old file whole: the settings file holds
// more than the unmounted section. The temporary file is created
// owner-only, the mode fileperm.WriteOwnerOnly gave it before.
func replaceFile(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// withoutSection drops a top-level section from a settings document: its
// key line, every line indented under it, and the comment line gofastr
// writes above it. A top-level key ends the section, and so does a
// column-0 comment unless more of the section's indented lines follow it:
// a comment between two entries belongs to the section.
func withoutSection(doc, name string) string {
	lines := strings.SplitAfter(doc, "\n")
	isTop := func(line string) bool {
		trimmed := strings.TrimRight(line, "\r\n")
		return trimmed != "" && trimmed[0] != ' ' && trimmed[0] != '\t'
	}
	isComment := func(line string) bool { return strings.HasPrefix(line, "#") }
	isBlank := func(line string) bool { return strings.TrimSpace(line) == "" }
	// continues reports whether the section goes on after line i: the next
	// line that is neither blank nor a column-0 comment is indented.
	continues := func(i int) bool {
		for _, line := range lines[i+1:] {
			if isBlank(line) || (isTop(line) && isComment(line)) {
				continue
			}
			return !isTop(line)
		}
		return false
	}
	var out []string
	in := false
	for i, line := range lines {
		top := isTop(line)
		if top && strings.HasPrefix(line, name+":") {
			in = true
			if n := len(out); n > 0 && out[n-1] == "# Written by gofastr unmount and gofastr remount.\n" {
				out = out[:n-1]
			}
			continue
		}
		if in {
			switch {
			case !top:
				continue
			case isComment(line) && continues(i):
				continue
			default:
				in = false
			}
		}
		out = append(out, line)
	}
	kept := strings.TrimRight(strings.Join(out, ""), "\n")
	if strings.TrimSpace(kept) == "" {
		return ""
	}
	return kept + "\n"
}
