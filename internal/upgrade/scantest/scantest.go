// Package scantest builds throwaway apps for tests that drive the
// upgrade scanner (internal/upgrade/scan) over registry notes.
//
// An app is a module in a temp directory. Notes whose matchers read
// strings, CSS, gofastr.yml, go.mod or other text need nothing else.
// Notes that name gofastr Go API need the old symbols to exist: pass a
// Kit, a stub of the gofastr module holding just those declarations at
// their import paths, and the app requires it through a replace
// directive, so the type checker resolves the app's uses against the
// stub exactly as it would against the old release.
//
// Every scan runs hermetic: no network, no workspace, modules resolved
// through replace directives only.
package scantest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// Options shape an app. The zero value is a module with no
// dependencies.
type Options struct {
	// Kit, when set, is a stub gofastr module: slash paths relative to
	// the module root ("framework/ui/ui.go") mapped to file contents. The
	// app requires it as upgrade.ModulePath through a replace.
	Kit map[string]string
}

// App writes an app module and returns its directory. files maps slash
// paths to contents; a "go.mod" entry replaces the generated one.
func App(t testing.TB, files map[string]string, opts Options) string {
	t.Helper()
	base := t.TempDir()
	appDir := filepath.Join(base, "app")
	gomod := "module example.com/app\n\ngo 1.27.0\n"
	if opts.Kit != nil {
		kitDir := filepath.Join(base, "kit")
		write(t, kitDir, "go.mod", "module "+upgrade.ModulePath+"\n\ngo 1.27.0\n")
		for rel, src := range opts.Kit {
			write(t, kitDir, rel, src)
		}
		gomod += "\nrequire " + upgrade.ModulePath + " v0.0.0\n\nreplace " + upgrade.ModulePath + " => ../kit\n"
	}
	if _, ok := files["go.mod"]; !ok {
		write(t, appDir, "go.mod", gomod)
	}
	for rel, src := range files {
		write(t, appDir, rel, src)
	}
	return appDir
}

// Run scans root for notes with the given marker sinks, in the hermetic
// environment, and fails the test when the scan cannot start.
func Run(t testing.TB, root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) *scan.Result {
	t.Helper()
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	res, err := scan.Run(root, notes, sinks)
	if err != nil {
		t.Fatalf("scan.Run: %v", err)
	}
	return res
}

// Hits renders a note's hits as "file:line:col why", in the scan's order.
func Hits(res *scan.Result, n *upgrade.Note) []string {
	out := make([]string, 0, len(res.Hits[n]))
	for _, h := range res.Hits[n] {
		out = append(out, fmt.Sprintf("%s:%d:%d %s", h.File, h.Line, h.Col, h.Why))
	}
	return out
}

// Note returns the index-th note (0-based) of release version in reg,
// failing the test when either is missing.
func Note(t testing.TB, reg *upgrade.Registry, version string, index int) *upgrade.Note {
	t.Helper()
	i := slices.IndexFunc(reg.Releases, func(r upgrade.Release) bool { return r.Version == version })
	if i < 0 {
		t.Fatalf("registry has no release %s", version)
	}
	notes := reg.Releases[i].Notes
	if index < 0 || index >= len(notes) {
		t.Fatalf("release %s has %d notes, no index %d", version, len(notes), index)
	}
	return notes[index]
}

// Only returns a copy of n whose Find keeps just the matcher kinds
// named (uses, imports, fields, strings, css, config, gomod, text). A
// test that checks one matcher kind in isolation strips the rest.
func Only(n *upgrade.Note, kinds ...string) *upgrade.Note {
	c := *n
	f := upgrade.Find{}
	for _, k := range kinds {
		switch k {
		case "uses":
			f.Uses = n.Find.Uses
		case "imports":
			f.Imports = n.Find.Imports
		case "fields":
			f.Fields = n.Find.Fields
		case "strings":
			f.Strings = n.Find.Strings
		case "css":
			f.CSS = n.Find.CSS
		case "config":
			f.Config = n.Find.Config
		case "gomod":
			f.GoMod = n.Find.GoMod
		case "text":
			f.Text = n.Find.Text
		default:
			panic("scantest.Only: unknown matcher kind " + k)
		}
	}
	c.Find = f
	return &c
}

// write creates rel under dir through an os.Root, so a path that would
// leave the module fails instead of landing elsewhere.
func write(t testing.TB, dir, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(rel), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("scantest: write %s: %v", rel, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
