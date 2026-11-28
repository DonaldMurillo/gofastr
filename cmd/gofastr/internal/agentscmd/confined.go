package agentscmd

import (
	"io/fs"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
)

func openProjectRoot(dir string) (*os.Root, error) { return projectfs.OpenRoot(dir) }

func readInRoot(root *os.Root, name string) ([]byte, error) { return projectfs.Read(root, name) }

func replaceInRoot(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	return projectfs.Replace(root, name, data, perm)
}

// ReadProjectFile reads name, slash-separated and relative to dir, and
// errors when a link on its path leads out of dir.
func ReadProjectFile(dir, name string) ([]byte, error) { return projectfs.ReadFile(dir, name) }

// WriteProjectFile replaces name under dir through a temporary file,
// following a link only while it stays in dir. An existing file keeps its
// mode; a new one gets 0644 less the umask.
func WriteProjectFile(dir, name string, data []byte) error {
	return projectfs.WriteFile(dir, name, data, 0o644)
}
