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

// fsListLimit caps the entries one fs.list call returns.
const fsListLimit = 10000

// AllowFolder grants read and list under the folder p: fs.stat,
// fs.readText and fs.list on p and on every path below it. p must be an
// absolute path to an existing directory. The grant stores p's clean
// form and its symlink resolution now; a request under either is opened
// through an os.Root at that resolution, so a symlink or ".." inside the
// folder cannot reach a file outside it. fs.writeText is never granted
// this way: a write needs the exact path from openFile or saveFile.
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

// folderGrant finds the picked folder p sits under and opens it as an
// os.Root, answering the root and p's path inside it ("." for the
// folder itself). p must arrive clean, the form the dialog returned, so
// an unclean spelling misses like it does on the exact list. The
// folder's live resolution must still be the one stored at the pick: a
// picked path swapped for a symlink since then names another folder.
// The caller closes the root.
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

// folderError maps an error from a read inside a picked folder: a
// missing path is not_found; anything else, an escape os.Root refused
// included, is the fixed denial, so the answer never says which.
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

// listFolder lists the directory rel inside root, sorted by name. Past
// fsListLimit entries it answers the first fsListLimit the directory
// yields and sets Truncated.
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
