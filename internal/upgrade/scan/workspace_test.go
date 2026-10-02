package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// testEnv pins the hermetic environment for every scan that shells out to
// the go tool: no network, no workspace, module resolution via replace.
func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
}

// defaultKit is the stub "example.com/kit" module: enough surface for every
// matcher's cases (a ui package with the symbols the registry spells, a
// registry with the marker-sink call, and the patterns packages the imports
// matcher names).
var defaultKit = map[string]string{
	"ui/ui.go": `package ui

type Attrs map[string]string

func SiteHeader(title string) string { return title }

type Layout struct{ Title string }

func (l *Layout) WithHeader(h string) {}

type ButtonConfig struct{ ExtraAttrs Attrs }

type FormConfig struct{ Action string }

type OtherConfig struct{ Action string }

type SidebarConfig struct{ DrawerName string }

type Embedded struct{ Promoted string }

type Wrapper struct{ Embedded }

type Stack[T any] struct{ Items []T }

func NewStack[T any]() *Stack[T] { return nil }
`,
	"registry/registry.go": `package registry

// RegisterStyle installs a style sheet under a component name.
func RegisterStyle(name, css string) {}
`,
	"patterns/accordion/accordion.go": `package accordion

func Render() string { return "" }
`,
	"patterns/tree/tree.go": `package tree

func Render() string { return "" }
`,
	"patterns/treeview/treeview.go": `package treeview

func Render() string { return "" }
`,
}

const defaultAppGoMod = `module example.com/app

go 1.27.0

require example.com/kit v0.1.0

replace example.com/kit => ../kit
`

// newWorkspace writes base/kit (defaultKit unless kit files are given) and
// base/app requiring it via replace. An app "go.mod" entry overrides the
// default. Returns the app directory.
func newWorkspace(t *testing.T, kit, app map[string]string) string {
	t.Helper()
	base := t.TempDir()
	kitDir := filepath.Join(base, "kit")
	writeWorkspaceFile(t, kitDir, "go.mod", "module example.com/kit\n\ngo 1.26.0\n")
	for rel, src := range kit {
		writeWorkspaceFile(t, kitDir, rel, src)
	}
	appDir := filepath.Join(base, "app")
	if _, ok := app["go.mod"]; !ok {
		writeWorkspaceFile(t, appDir, "go.mod", defaultAppGoMod)
	}
	for rel, src := range app {
		writeWorkspaceFile(t, appDir, rel, src)
	}
	return appDir
}

func writeWorkspaceFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lineOf and colOf are the 1-based position of needle's first occurrence in
// src, so tests assert hit positions against the exact sources they wrote.
func lineOf(src, needle string) int {
	i := strings.Index(src, needle)
	if i < 0 {
		panic("needle not in src: " + needle)
	}
	return 1 + strings.Count(src[:i], "\n")
}

func colOf(src, needle string) int {
	i := strings.Index(src, needle)
	if i < 0 {
		panic("needle not in src: " + needle)
	}
	return i - strings.LastIndex(src[:i], "\n")
}

// hitAt renders the expected hit for needle in src as file:line:col why.
func hitAt(src, needle, file, why string) string {
	return fmt.Sprintf("%s:%d:%d %s", file, lineOf(src, needle), colOf(src, needle), why)
}

func hitStrs(hs []Hit) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = fmt.Sprintf("%s:%d:%d %s", h.File, h.Line, h.Col, h.Why)
	}
	return out
}

// wantHits asserts the note's rendered hits, in order.
func wantHits(t *testing.T, res *Result, n *upgrade.Note, want ...string) {
	t.Helper()
	got := hitStrs(res.Hits[n])
	if len(got) != len(want) {
		t.Fatalf("hits:\n got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hits:\n got %v\nwant %v", got, want)
		}
	}
}

// mustRun runs the engine over the workspace with one note.
func mustRun(t *testing.T, root string, notes ...*upgrade.Note) *Result {
	t.Helper()
	testEnv(t)
	res, err := Run(root, notes, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// testSinks are the marker sinks the stub kit offers: RegisterStyle's first
// argument takes a component name, SidebarConfig.DrawerName holds one, and
// data-fui-comp map values are component markers.
func testSinks() upgrade.MarkerSinks {
	return upgrade.MarkerSinks{
		Calls: []upgrade.ParamSink{{
			Func: upgrade.Symbol{Pkg: "example.com/kit/registry", Name: "RegisterStyle"},
			Arg:  0,
		}},
		Fields: []upgrade.Symbol{{
			Pkg: "example.com/kit/ui", Name: "SidebarConfig", Member: "DrawerName",
		}},
		AttrKeys: []string{"data-fui-comp"},
	}
}
