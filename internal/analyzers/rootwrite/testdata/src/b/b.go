// Package b holds rootwrite positives in code that never existed in
// the repo: different names, same shape.
package b

import (
	"archive/zip"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// installPlugin unpacks a plugin under baseDir. A symlinked
// "plugins/<name>" component writes outside baseDir undetected.
func installPlugin(baseDir, name string) error {
	if err := os.MkdirAll(filepath.Join(baseDir, "plugins", name), 0o755); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	f, err := os.Create(filepath.Join(baseDir, "plugins", name, "binary")) // want `write under a root with lexical containment only`
	if err != nil {
		return err
	}
	return f.Close()
}

// markVerified stamps a file with an exclusive-create open so a second
// run refuses to clobber — same lexical-only containment.
func markVerified(workdir, id string) error {
	f, err := os.OpenFile(filepath.Join(workdir, id+".verified"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) // want `write under a root with lexical containment only`
	if err != nil {
		return err
	}
	return f.Close()
}

// installPluginFixed resolves both sides before creating anything.
func installPluginFixed(baseDir, name string) error {
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	target := filepath.Join(realBase, "plugins", name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(target, "binary"))
	if err != nil {
		return err
	}
	return f.Close()
}

// archiveSessions writes one archive entry per log file, with the
// session parameter concatenated into the entry name and no Clean.
func archiveSessions(zw *zip.Writer, session string, logs []string) error {
	for _, l := range logs {
		w, err := zw.Create(session + "/" + l) // want `zip entry name built from a parameter without path.Clean`
		if err != nil {
			return err
		}
		if _, err := w.Write(nil); err != nil {
			return err
		}
	}
	return nil
}

// archiveSessionsFixed cleans the caller-supplied prefix first.
func archiveSessionsFixed(zw *zip.Writer, session string, logs []string) error {
	clean := path.Clean(session)
	if clean == ".." || clean == "." || len(clean) >= 2 && clean[:2] == ".." || session == "" {
		return fmt.Errorf("unsafe session prefix %q", session)
	}
	for _, l := range logs {
		w, err := zw.Create(clean + "/" + l)
		if err != nil {
			return err
		}
		if _, err := w.Write(nil); err != nil {
			return err
		}
	}
	return nil
}

// openForRead reads under the root: O_RDONLY is not a write, and the
// rule stays quiet by construction.
func openForRead(workdir, id string) ([]byte, error) {
	f, err := os.OpenFile(filepath.Join(workdir, id), os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return nil, f.Close()
}
