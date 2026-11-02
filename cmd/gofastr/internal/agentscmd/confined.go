package agentscmd

import (
	"io/fs"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
)

// The onboarding files this package writes (AGENTS.md, the .agents/
// detail files, the gofastr-host skill) land in a project the user may
// have cloned from anyone. Every read and write goes through an os.Root
// opened on the project directory (cmd/gofastr/internal/projectfs), so a
// symlink in it, whether a directory on the way or the file itself,
// cannot carry a write outside the project: a link that leaves it is
// refused. A link that stays in the project is followed, as a write
// through it would be.

// openProjectRoot opens dir, creating it first, as the root every
// onboarding write is confined to.
func openProjectRoot(dir string) (*os.Root, error) { return projectfs.OpenRoot(dir) }

// readInRoot reads name under root, following a link only while it stays
// in root.
func readInRoot(root *os.Root, name string) ([]byte, error) { return projectfs.Read(root, name) }

// replaceInRoot replaces name under root with data (projectfs.Replace):
// through a temporary file, a link followed only while it stays in root,
// an existing file's mode kept and a new one created with perm less the
// umask.
func replaceInRoot(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	return projectfs.Replace(root, name, data, perm)
}

// ReadProjectFile reads name (slash-separated, relative to dir) through an
// os.Root on dir (projectfs.ReadFile): a project file linked out of the
// project reads as an error, never as the file it points at. `gofastr
// init --reinit` reads AGENTS.md and CLAUDE.md through it.
func ReadProjectFile(dir, name string) ([]byte, error) { return projectfs.ReadFile(dir, name) }

// WriteProjectFile replaces name (slash-separated, relative to dir) with
// data the way the agents writers do (projectfs.WriteFile at 0644): a
// link inside dir is written through, one out of dir is refused, an
// existing file keeps its mode and a new one gets 0644 less the umask.
// `gofastr init --reinit` writes AGENTS.md, CLAUDE.md and DESIGN.md
// through it.
func WriteProjectFile(dir, name string, data []byte) error {
	return projectfs.WriteFile(dir, name, data, 0o644)
}
