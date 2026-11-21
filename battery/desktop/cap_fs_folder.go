package desktop

import (
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const fsListLimit = 10000

// AllowFolder grants read and list under the folder p: fs.stat,
// fs.readText and fs.list on p and on every path below it.
func (b *Battery) AllowFolder(p string) error {
	if !filepath.IsAbs(p) {
		return errors.New("desktop: AllowFolder requires an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("desktop: AllowFolder requires a directory")
	}
	b.allowMu.Lock()
	b.allowFolders[filepath.Clean(p)] = resolved
	b.allowFolders[resolved] = resolved
	b.allowMu.Unlock()
	return nil
}

func (b *Battery) folderGrant(p string) (*os.Root, string, bool) {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return nil, "", false
	}
	b.allowMu.Lock()
	folders := maps.Clone(b.allowFolders)
	b.allowMu.Unlock()
	for _, folder := range slices.Sorted(maps.Keys(folders)) {
		rel, err := filepath.Rel(folder, p)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		resolved := folders[folder]
		if live, err := filepath.EvalSymlinks(folder); err != nil || live != resolved {
			continue
		}
		root, err := os.OpenRoot(resolved)
		if err != nil {
			continue
		}
		return root, rel, true
	}
	return nil, "", false
}

func folderError(err error) *Error {
	if errors.Is(err, fs.ErrNotExist) {
		return &Error{Code: CodeNotFound, Message: "path does not exist"}
	}
	return errPathNotAllowed
}

type fsListEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

type fsListOutput struct {
	Entries   []fsListEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

func listFolder(root *os.Root, rel string) (fsListOutput, *Error) {
	d, err := root.Open(rel)
	if err != nil {
		return fsListOutput{}, folderError(err)
	}
	defer d.Close()
	entries, err := d.ReadDir(fsListLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return fsListOutput{}, folderError(err)
	}
	out := fsListOutput{Entries: []fsListEntry{}}
	if len(entries) > fsListLimit {
		entries, out.Truncated = entries[:fsListLimit], true
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, fsListEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	slices.SortFunc(out.Entries, func(a, b fsListEntry) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}
