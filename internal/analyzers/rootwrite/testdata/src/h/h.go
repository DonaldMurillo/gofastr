// Package h pins bug bash 2026-10-27 I4: the static export
// (framework/static/builder.go:180-184) joins
// dst := filepath.Join(b.OutDir, pathToFile(p)) inside a walk callback,
// checks it with the lexical ensureContained (:799-806), then hands it
// to writeFile (:1782-1786: MkdirAll(Dir(dst)) + os.WriteFile(dst)). An
// existing OutDir/blog symlink gets index.html written through it.
// pathflow stopped at writeFile's plain parameter, and the callback's
// own parameter p was not counted as caller data: the sink was
// invisible twice over.
package h

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Builder struct {
	OutDir string
}

func walk(fn func(route, p, page string) error) error { return fn("/", "/blog", "<html>") }

func pathToFile(p string) string {
	return filepath.FromSlash(strings.TrimPrefix(p, "/")) + "/index.html"
}

func (b *Builder) ensureContained(dst string) error {
	rel, err := filepath.Rel(b.OutDir, dst)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("refusing to write outside OutDir")
	}
	return nil
}

// writeFile is the sink helper: its own writes take a bare parameter.
func writeFile(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// Build is the I4 shape: the write happens one helper deep, from a
// callback.
func (b *Builder) Build() error {
	return walk(func(route, p, page string) error {
		dst := filepath.Join(b.OutDir, pathToFile(p))
		if err := b.ensureContained(dst); err != nil {
			return err
		}
		return writeFile(dst, []byte(page)) // want `write under a root with lexical containment only.*writeFile`
	})
}

// writePage is the same through a method parameter.
func (b *Builder) writePage(p string, html []byte) error {
	dst := filepath.Join(b.OutDir, pathToFile(p))
	return writeFile(dst, html) // want `write under a root with lexical containment only.*writeFile`
}

// writeTwice reaches the sink two helpers deep.
func writeTwice(dst string, data []byte) error {
	if err := writeFile(dst, data); err != nil {
		return err
	}
	return writeFile(dst+".bak", data)
}

func (b *Builder) backup(p string, html []byte) error {
	return writeTwice(filepath.Join(b.OutDir, p), html) // want `write under a root with lexical containment only.*writeTwice`
}

// writeHeadersFile joins a literal under its root: nothing
// caller-controlled lands under it. Quiet.
func writeHeadersFile(outDir, body string) error {
	return writeFile(filepath.Join(outDir, "_headers"), []byte(body))
}

// resolvedWrite resolves the parent chain before writing: the helper
// is the fix posture, so its callers are quiet.
func resolvedWrite(dst string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Dir(dst))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(real, filepath.Base(dst)), data, 0o644)
}

func (b *Builder) writeResolved(p string, html []byte) error {
	return resolvedWrite(filepath.Join(b.OutDir, p), html)
}

// writeViaRoot is the os.Root fix: kernel-enforced containment. Quiet.
func (b *Builder) writeViaRoot(p string, html []byte) error {
	root, err := os.OpenRoot(b.OutDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(pathToFile(p), html, 0o644)
}

// replaceFile only clobbers the leaf.
func replaceFile(dst string, data []byte) error {
	return os.WriteFile(dst, data, 0o644)
}

// leafChecked Lstats the leaf before handing it to a leaf-only helper:
// the documented partial fix. Quiet.
func (b *Builder) leafChecked(p string, html []byte) error {
	dst := filepath.Join(b.OutDir, p)
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return replaceFile(dst, html)
}

// leafCheckedMkdir: the same leaf check does not cover writeFile's
// MkdirAll of the parent chain.
func (b *Builder) leafCheckedMkdir(p string, html []byte) error {
	dst := filepath.Join(b.OutDir, p)
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return writeFile(dst, html) // want `write under a root with lexical containment only.*writeFile`
}

// readHelper takes a path parameter and only reads it: no sink.
func readHelper(src string) ([]byte, error) { return os.ReadFile(src) }

func (b *Builder) readBack(p string) ([]byte, error) {
	return readHelper(filepath.Join(b.OutDir, p))
}

// literalCallback: a callback whose components are literals only.
func (b *Builder) literalCallback() error {
	return walk(func(route, p, page string) error {
		return writeFile(filepath.Join(b.OutDir, "404.html"), []byte(page))
	})
}

// checkedWrite resolves its own parameter's parent chain before
// writing it: the helper is the fix posture. Quiet at the caller.
func checkedWrite(dst string, data []byte) error {
	if _, err := filepath.EvalSymlinks(filepath.Dir(dst)); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func (b *Builder) writeChecked(p string, html []byte) error {
	return checkedWrite(filepath.Join(b.OutDir, p), html)
}

func phase(maintenance bool) string {
	if maintenance {
		return "maintenance"
	}
	return "initial"
}

// writePhase: the only caller data is a bool, which carries no path
// text (evals grader.go:75 serverLog). Quiet.
func (b *Builder) writePhase(maintenance bool, html []byte) error {
	return writeFile(filepath.Join(b.OutDir, phase(maintenance)+"-server.log"), html)
}

// writeRun: a counter is no caller-controlled component either. Quiet.
func (b *Builder) writeRun(n int, html []byte) error {
	return writeFile(filepath.Join(b.OutDir, "run", string(rune('a'+n))), html)
}
