// Package settings reads gofastr.settings.yml, the one committed file that
// holds a project's gofastr configuration. Each top-level section belongs
// to one reader: codegen to `gofastr generate`, contracts to `gofastr
// verify`, isolation to the running app, unmounted to the CLI's record of
// copied packages. This package finds the file and checks its top level;
// each reader decodes its own section. The unmounted section is decoded
// and written here, since several commands share it.
package settings

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// FileName is the settings file's name at the project root.
const FileName = "gofastr.settings.yml"

// sections are the top-level keys the file may hold. version is the file
// format's version; the rest are one reader each.
var sections = map[string]bool{
	"version":   true,
	"codegen":   true,
	"contracts": true,
	"isolation": true,
	"unmounted": true,
}

// IsSection reports whether name is one of the file's reader sections, for
// a reader of another file that wants to point a misplaced key here.
func IsSection(name string) bool { return name != "version" && sections[name] }

// File is a parsed settings file.
type File struct {
	// Path is the file's path, absolute when Find located it.
	Path string
	// Dir is the directory holding the file: the project root its
	// relative paths resolve against.
	Dir  string
	root *coreyaml.Node
}

// Section returns the named top-level section, or nil when the file has
// none.
func (f File) Section(name string) *coreyaml.Node {
	if f.root == nil || f.root.Kind != coreyaml.Map {
		return nil
	}
	return f.root.Map[name]
}

// Root returns the whole parsed document, for a reader whose decoder takes
// the file with its section in place.
func (f File) Root() *coreyaml.Node { return f.root }

// retired are the files this one replaced, by the section their content
// moved to. Each reader refuses its own: a file that is no longer read
// would otherwise drop its settings without a word.
var retired = map[string][]string{
	"codegen":   {"gofastr.codegen.yml", "gofastr.codegen.yaml"},
	"contracts": {"gofastr.contracts.yml", "gofastr.contracts.yaml"},
	"isolation": {"gofastr.isolation.yml", "gofastr.isolation.yaml"},
}

// Find looks for FileName in start and each parent up to the project root
// (the first directory holding go.work, go.mod or .git), never past it. A
// settings file can name a command that generate runs, so one planted in a
// shared ancestor (a workspace parent, /tmp, $HOME) must never apply. With
// no project root above start, only start itself is searched. ok is false
// when no settings file is found.
//
// section names the reader. A file that used to hold that section on its
// own (gofastr.codegen.yml for codegen) is an error naming where its
// content goes now.
func Find(start, section string) (f File, ok bool, err error) {
	dirs, err := SearchDirs(start)
	if err != nil {
		return File{}, false, err
	}
	for _, dir := range dirs {
		for _, name := range retired[section] {
			old := filepath.Join(dir, name)
			if _, err := os.Stat(old); err == nil {
				return File{}, false, fmt.Errorf("%s is no longer read: move its content under %q in %s", old, section+":", filepath.Join(dir, FileName))
			}
		}
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, FileName)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return File{}, false, err
		}
		f, err := Load(path)
		if err != nil {
			return File{}, false, err
		}
		return f, true, nil
	}
	return File{}, false, nil
}

// Load reads and checks the settings file at path.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	return Parse(path, data)
}

// Parse checks a settings document: a map whose keys are known sections,
// with version 1 when a version is given. An empty document is a file
// with no sections.
func Parse(path string, data []byte) (File, error) {
	root, err := coreyaml.Parse(string(data))
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if root.Kind != coreyaml.Map {
		return File{}, fmt.Errorf("%s: the settings file must be a map of sections (%s)", path, sectionList())
	}
	for _, key := range slices.Sorted(maps.Keys(root.Map)) {
		if !sections[key] {
			return File{}, fmt.Errorf("%s: unknown section %q; the settings file holds %s", path, key, sectionList())
		}
	}
	if v := root.Map["version"]; v != nil {
		if n, ok := v.Value.(int64); v.Kind != coreyaml.Scalar || !ok || n != 1 {
			return File{}, fmt.Errorf("%s: version %v is not supported; the settings file format is version 1", path, v.Value)
		}
	}
	return File{Path: path, Dir: filepath.Dir(path), root: root}, nil
}

func sectionList() string {
	var names []string
	for _, k := range slices.Sorted(maps.Keys(sections)) {
		if k != "version" {
			names = append(names, k)
		}
	}
	return strings.Join(names, ", ")
}

// SearchDirs lists the directories Find searches: start and each parent
// up to and including the project root. With no root marker anywhere
// above start it returns start alone: an unmarked tree says nothing about
// where the project ends.
func SearchDirs(start string) ([]string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for {
		dirs = append(dirs, dir)
		if IsProjectRoot(dir) {
			return dirs, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs[:1], nil
}

// projectRootMarkers are the files that make a directory a project root.
var projectRootMarkers = []string{"go.work", "go.mod", ".git"}

// IsProjectRoot reports whether dir holds a project root marker.
func IsProjectRoot(dir string) bool {
	for _, m := range projectRootMarkers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}
