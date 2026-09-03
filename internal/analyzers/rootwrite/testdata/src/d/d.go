// Package d holds the rootwrite spellings the 2026-09-02 adversarial
// review showed were missing or mis-scoped: a rooty field copied into
// a local, a helper called with literal-only components, validator /
// EvalSymlinks / Clean gates that never touch the write path, and
// concatenation-built paths.
package d

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rootLocal: the rooty field is validated through a helper first and
// the JOIN sees only the local — the write is still lexically
// contained only.
func rootLocal(token string, data []byte) error {
	v := vault{}
	root, err := resolveRoot(v.root)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, token), data, 0o600) // want `write under a root with lexical containment only`
}

type vault struct{ root string }

func resolveRoot(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty root")
	}
	return s, nil
}

// containedHelper joins and prefix-checks: lexical containment only.
func containedHelper(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute")
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	base := filepath.Clean(root)
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", fmt.Errorf("escapes")
	}
	return abs, nil
}

// helperLiteral: every non-root argument of the helper call is a
// literal — a fixed manifest name through a containment helper is a
// real idiom and nothing caller-controlled is appended.
func helperLiteral(r Report) error {
	abs, err := containedHelper(r.Root, "manifest.json")
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte("{}"), 0o644)
}

type Report struct{ Root string }

// helperParam: the helper call carries a parameter beside the root:
// fires like a direct Join.
func helperParam(r Report, rel string) error {
	abs, err := containedHelper(r.Root, rel)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte("{}"), 0o644) // want `write under a root with lexical containment only`
}

// validName is a name-FORMAT validator: it cannot see symlinks.
func validName(s string) bool {
	for _, r := range s {
		if r == 0 || r > 127 {
			return false
		}
	}
	return s != ""
}

// validGate: a format validator consulted for its boolean does not
// resolve the symlinked components of the join.
func validGate(baseDir, name string) error {
	if !validName(name) {
		return errors.New("bad name")
	}
	return os.MkdirAll(filepath.Join(baseDir, "plugins", name), 0o755) // want `write under a root with lexical containment only`
}

// validResultFixed: the sanitizer's RESULT replaces the joined
// component — that is the dataflow the silence is declared on.
func validResultFixed(baseDir, name string) error {
	safe := sanitizeName(name)
	if safe == "" {
		return errors.New("bad name")
	}
	return os.MkdirAll(filepath.Join(baseDir, "plugins", safe), 0o755)
}

// cleanShield: filepath.Clean normalizes separators and dot segments;
// it resolves no symlink, so the symlinked-directory escape this rule
// reports survives it untouched.
func cleanShield(root, name string, data []byte) error {
	return os.WriteFile(filepath.Join(root, filepath.Clean(name)), data, 0o600) // want `write under a root with lexical containment only`
}

// cleanNameKeep is a same-package helper whose RESULT replaces the
// joined component — the local clean-named spelling that keeps
// shielding (a stdlib Clean never does).
func cleanNameKeep(s string) string {
	return strings.TrimSuffix(s, "/")
}

func cleanLocalShield(root, name string, data []byte) error {
	return os.WriteFile(filepath.Join(root, cleanNameKeep(name)), data, 0o600)
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0 && r <= 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// evalAnywhere: EvalSymlinks resolved an unrelated path; the write's
// own chain is untouched.
func evalAnywhere(token string, data []byte) error {
	v := vault{}
	audit, err := filepath.EvalSymlinks(filepath.Join(v.root, "audit.log"))
	if err != nil {
		return err
	}
	_ = audit
	return os.WriteFile(filepath.Join(v.root, token), data, 0o600) // want `write under a root with lexical containment only`
}

// zipClean: a Clean on an unrelated disk path cleans no entry name.
func zipClean(session string, logs []string) ([]byte, error) {
	cleanDisk := filepath.Clean("/var/tmp//x")
	_ = cleanDisk
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, l := range logs {
		if _, err := w.Create(session + "/" + l); err != nil { // want `zip entry name built from a parameter without path.Clean`
			return nil, err
		}
	}
	return buf.Bytes(), w.Close()
}

// concatBuild: a flat root + "/" + x chain is the same lexical-only
// containment as a Join.
func concatBuild(root, name string, data []byte) error {
	return os.WriteFile(root+"/"+name, data, 0o600) // want `write under a root with lexical containment only`
}

// concatLiteral: nothing caller-controlled after the root.
func concatLiteral(root string) error {
	return os.WriteFile(root+"/manifest.json", []byte("{}"), 0o644)
}
