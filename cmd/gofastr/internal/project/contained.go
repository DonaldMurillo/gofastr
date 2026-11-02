package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// RefuseEscape refuses a write to rel under dir that would land outside
// dir: a directory component that is a symlink leaving dir, or a leaf
// that is a symlink leaving dir or naming nothing (writing through a
// dangling link creates its target, wherever it points). A project
// cloned from elsewhere can carry such links, and the scaffold and
// onboarding writers (gofastr init, gofastr agents) follow them with
// os.WriteFile, so `.claude/skills/gofastr-host/SKILL.md -> ~/.bashrc`
// replaced the developer's file with the skill. A link that stays inside
// dir is followed as before (a CLAUDE.md linked to AGENTS.md stays a
// link). A rel that does not exist yet passes: its missing directories
// are created inside dir.
//
// Call it before creating any directory on the way to rel, so a
// symlinked parent is refused before os.MkdirAll creates directories
// through it.
func RefuseEscape(dir, rel string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	rel = filepath.Clean(rel)
	fi, err := root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("refusing to write %s: it resolves outside %s through a symlink (%w)", filepath.Join(dir, rel), dir, err)
	case fi.Mode()&fs.ModeSymlink != 0:
		if _, err := root.Stat(rel); err != nil {
			return fmt.Errorf("refusing to write %s: it is a symlink that leaves %s or names nothing (%w)", filepath.Join(dir, rel), dir, err)
		}
	}
	return nil
}
