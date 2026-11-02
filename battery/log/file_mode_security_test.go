//go:build !windows

package log

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileSinkTightensExistingLooseFile pins FileMode on a log file that
// already exists. The open's mode argument applies only at create, so a
// server.log left 0644 by an earlier build (before the 0600 default), a
// manual touch, or a provisioning script stayed world-readable for as
// long as it lived, while the sink's own rotation created every later
// file at FileMode. The sink now tightens the file to FileMode when it
// opens it, and never loosens one that is already tighter.
func TestFileSinkTightensExistingLooseFile(t *testing.T) {
	cases := []struct {
		name     string
		existing os.FileMode
		opt      os.FileMode
		want     os.FileMode
	}{
		{"default tightens 0644", 0o644, 0, 0o600},
		{"explicit 0640 tightens 0666", 0o666, 0o640, 0o640},
		{"never loosens", 0o600, 0o644, 0o600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server.log")
			if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, c.existing); err != nil {
				t.Fatal(err)
			}
			s, err := FileSink(path, FileOpts{FileMode: c.opt})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != c.want {
				t.Errorf("mode after FileSink = %o, want %o", got, c.want)
			}
			b, _ := os.ReadFile(path)
			if string(b) != "old\n" {
				t.Errorf("existing content changed: %q", b)
			}
		})
	}
}
