package sourcewalk

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func Walk(root string, visit fs.WalkDirFunc) error {
	root = filepath.Clean(root)
	seen := map[string]bool{}
	visitError := func(path string, entry fs.DirEntry, err error) error {
		err = visit(path, entry, err)
		if err == fs.SkipDir && entry.IsDir() {
			return nil
		}
		return err
	}
	var walk func(string, fs.DirEntry) error
	walk = func(path string, entry fs.DirEntry) error {
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(path)
			if err != nil {
				return visitError(path, entry, err)
			}
			if info.IsDir() {
				entry = fs.FileInfoToDirEntry(info)
			}
		}
		if entry.IsDir() && path != root && excluded(filepath.Base(path)) {
			return nil
		}
		canonical := ""
		if entry.IsDir() {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return visitError(path, entry, err)
			}
			canonical, err = filepath.Abs(resolved)
			if err != nil {
				return visitError(path, entry, err)
			}
			if seen[canonical] {
				return nil
			}
		}
		if err := visit(path, entry, nil); err != nil {
			if err == fs.SkipDir && entry.IsDir() {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		seen[canonical] = true
		entries, err := os.ReadDir(path)
		if err != nil {
			return visitError(path, entry, err)
		}
		for _, child := range entries {
			if err := walk(filepath.Join(path, child.Name()), child); err != nil {
				if err == fs.SkipDir {
					break
				}
				return err
			}
		}
		return nil
	}
	info, err := os.Lstat(root)
	if err != nil {
		err = visit(root, nil, err)
	} else {
		err = walk(root, fs.FileInfoToDirEntry(info))
	}
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

func excluded(name string) bool {
	switch name {
	case "vendor", "node_modules", "dist", "bin", "build", "tmp":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}
