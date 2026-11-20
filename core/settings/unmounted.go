package settings

import (
	"cmp"
	"errors"
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
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"golang.org/x/mod/module"
)

// Unmounted is one entry of the unmounted section: a gofastr package the
// project copied into its own tree with `gofastr unmount`.
type Unmounted struct {
	// Source is the upstream import path the copy was made from.
	Source string
	// Version is the gofastr version copied: a release tag, or dev.
	Version string
	// Path is the copy's directory, relative to the settings file.
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
		for _, entry := range []struct {
			key string
			val *string
		}{{"source", &u.Source}, {"version", &u.Version}, {"path", &u.Path}, {"date", &u.Date}} {
			key, dst := entry.key, entry.val
			n := item.Map[key]
			if n == nil {
				return nil, fmt.Errorf("%s: %s is required", at, key)
			}
			s, ok := "", false
			if n.Kind == coreyaml.Scalar {
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
		if seen[u.Path] {
			return nil, fmt.Errorf("%s: %s is listed twice", at, u.Path)
		}
		seen[u.Path] = true
		out = append(out, u)
	}
	return out, nil
}

// Validate checks one entry: an import path, a plain version, a clean
// relative path outside vendor/ and a YYYY-MM-DD date.
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

const unmountedHeader = "# Written by gofastr unmount and gofastr remount."

// SetUnmounted replaces the unmounted section of the settings file at p with
// entries, creating the file when needed and removing it when nothing is
// left. It errors when the result would not read back as exactly entries.
func SetUnmounted(p string, entries []Unmounted) error {
	for _, u := range entries {
		if err := u.Validate(); err != nil {
			return err
		}
	}
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b Unmounted) int {
		return cmp.Or(strings.Compare(a.Source, b.Source), strings.Compare(a.Path, b.Path))
	})
	var body strings.Builder
	if len(entries) > 0 {
		body.WriteString("unmounted:\n")
		for _, u := range entries {
			fmt.Fprintf(&body, "  - source: %s\n    version: %s\n    path: %s\n    date: %s\n",
				strconv.Quote(u.Source), strconv.Quote(u.Version), strconv.Quote(u.Path), strconv.Quote(u.Date))
		}
	}
	return setSection(p, "unmounted", unmountedHeader, body.String(), func(f File) error {
		got, err := f.Unmounted()
		if err != nil {
			return err
		}
		if !slices.Equal(got, entries) {
			return fmt.Errorf("%s: could not rewrite its unmounted section; write the key as a plain `unmounted:` at the start of a line", p)
		}
		return nil
	})
}

func setSection(p, name, header, body string, check func(File) error) error {
	stateDir := filepath.Join(filepath.Dir(p), ".gofastr")
	if err := os.Mkdir(stateDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, filepath.Base(p)+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return err
	}

	if info, err := os.Lstat(p); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink: gofastr writes the %s section into the file itself, so put the file there instead of the link", p, name)
	}
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) && body == "" {
		return nil
	}
	kept := withoutSection(string(data), name, header)
	var b strings.Builder
	b.WriteString(kept)
	if body != "" {
		if kept != "" && !strings.HasSuffix(kept, "\n") {
			b.WriteString("\n")
		}
		if kept != "" && !strings.HasSuffix(kept, "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString(header + "\n")
		b.WriteString(body)
	}
	out := b.String()
	if out == "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	f, err := Parse(p, []byte(out))
	if err != nil {
		return err
	}
	if err := check(f); err != nil {
		return err
	}
	return replaceFile(p, []byte(out))
}

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

func withoutSection(doc, name, header string) string {
	lines := strings.SplitAfter(doc, "\n")
	isTop := func(line string) bool {
		trimmed := strings.TrimRight(line, "\r\n")
		return trimmed != "" && trimmed[0] != ' ' && trimmed[0] != '\t'
	}
	isComment := func(line string) bool { return strings.HasPrefix(line, "#") }
	isBlank := func(line string) bool { return strings.TrimSpace(line) == "" }
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
			if n := len(out); n > 0 && strings.TrimRight(out[n-1], "\r\n") == header {
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
