package unmountcmd

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestUnmountSourcePathForms(t *testing.T) {
	for arg, want := range map[string]string{
		"battery/auth":                             project.GofastrModule + "/battery/auth",
		project.GofastrModule + "/battery/auth":    project.GofastrModule + "/battery/auth",
		project.GofastrModule + "/battery/auth/":   project.GofastrModule + "/battery/auth",
		"framework/ui/theme":                       project.GofastrModule + "/framework/ui/theme",
		project.GofastrModule + "/core/textsafe/x": project.GofastrModule + "/core/textsafe/x",
	} {
		got, err := unmountSourcePath(arg)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", arg, got, err, want)
		}
	}
}

func TestUnmountSourcePathRefusals(t *testing.T) {
	for arg, want := range map[string]string{
		"example.com/other/pkg": "not a gofastr package",
		"internal/fileperm":     "internal to gofastr",
		"battery/internal/x":    "internal to gofastr",
		"core/unmounted":        "registry every copy reports to",
		"../battery/auth":       "not a gofastr package",
		"battery//auth":         "not a package import path",
	} {
		_, err := unmountSourcePath(arg)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", arg, err, want)
		}
	}
}

func testUnmountTree() appgen.UnmountTree {
	src := project.GofastrModule + "/battery/demo"
	return appgen.UnmountTree{Source: src, Version: "v0.91.0", Files: []appgen.PackageFile{
		// Sorted ahead of demo.go: neither the external test package nor
		// the ignored program may name the package.
		{Name: "a_test.go", Content: "package demo_test\n"},
		{Name: "aa_gen.go", Content: "//go:build ignore\n\npackage main\n"},
		{Name: "demo.go", Content: "// Package demo is a demo.\npackage demo\n\nimport _ \"" + src + "/sub\"\n"},
		{Name: "static/app.css", Content: "body { color: red; }\n"},
		{Name: "sub/sub.go", Content: "package sub\n"},
	}}
}

func TestBuildUnmountCopyMarksAndHooks(t *testing.T) {
	tree := testUnmountTree()
	files, err := appgen.BuildUnmountCopy(tree, "example.com/app/unmounted/battery/demo")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[f.Name] = f.Content
	}
	for _, name := range []string{"demo.go", "sub/sub.go", appgen.UnmountHookFile, "sub/" + appgen.UnmountHookFile} {
		m, _, ok := codegen.ReadMarker(name, byName[name])
		if !ok || m.Generator != project.UnmountGenerator || m.Source != tree.Source || m.Version != "v0.91.0" {
			t.Errorf("%s marker = %+v ok=%v", name, m, ok)
		}
	}
	if !strings.Contains(byName["demo.go"], `"example.com/app/unmounted/battery/demo/sub"`) {
		t.Errorf("self-import not rewritten:\n%s", byName["demo.go"])
	}
	if byName["static/app.css"] != tree.Files[3].Content {
		t.Errorf("an asset was changed: %q", byName["static/app.css"])
	}
	root := byName[appgen.UnmountHookFile]
	if !strings.HasPrefix(strings.SplitN(root, "\n\n", 2)[1], "package demo\n") {
		t.Errorf("root hook names the wrong package (the test file and the ignored program say otherwise):\n%s", root)
	}
	if !strings.Contains(byName["sub/"+appgen.UnmountHookFile], "package sub\n") {
		t.Errorf("sub hook:\n%s", byName["sub/"+appgen.UnmountHookFile])
	}
	listed := readUnmountFiles(root)
	if len(listed) != len(files)-1 || listed["static/app.css"] != codegen.Sum(tree.Files[3].Content) {
		t.Errorf("listed = %v", listed)
	}
	if _, ok := listed[appgen.UnmountHookFile]; ok {
		t.Error("the root hook lists itself")
	}
}

func writeUnmountCopy(t *testing.T, dir string) {
	t.Helper()
	files, err := appgen.BuildUnmountCopy(testUnmountTree(), "example.com/app/unmounted/battery/demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(dir, "unmounted", "battery", "demo", filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, p, f.Content)
	}
}

func TestUnmountCopyChanges(t *testing.T) {
	entry := settings.Unmounted{Source: project.GofastrModule + "/battery/demo", Version: "v0.91.0", Path: "unmounted/battery/demo", Date: "2026-10-19"}
	copyDir := filepath.Join("unmounted", "battery", "demo")
	for _, tc := range []struct {
		name   string
		change func(dir string)
		want   []string
	}{
		{"untouched", func(string) {}, nil},
		{"edited go", func(d string) {
			appendTestFile(t, filepath.Join(d, copyDir, "demo.go"), "// mine\n")
		}, []string{"demo.go (edited)"}},
		{"edited asset", func(d string) {
			appendTestFile(t, filepath.Join(d, copyDir, "static", "app.css"), "a{}\n")
		}, []string{"static/app.css (edited)"}},
		{"added", func(d string) {
			clitest.WriteTestFile(t, filepath.Join(d, copyDir, "sub", "extra.go"), "package sub\n")
		}, []string{"sub/extra.go (added)"}},
		{"deleted", func(d string) {
			if err := os.Remove(filepath.Join(d, copyDir, "sub", "sub.go")); err != nil {
				t.Fatal(err)
			}
		}, []string{"sub/sub.go (deleted)"}},
		{"hook edited", func(d string) {
			appendTestFile(t, filepath.Join(d, copyDir, appgen.UnmountHookFile), "// mine\n")
		}, []string{appgen.UnmountHookFile + " (edited)", "demo.go (added)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeUnmountCopy(t, dir)
			tc.change(dir)
			got, err := unmountCopyChanges(dir, entry)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(strings.Join(got, "\n"), w) {
					t.Errorf("changes = %v, want %s", got, w)
				}
			}
			if len(tc.want) == 0 && len(got) > 0 {
				t.Errorf("an untouched copy reports changes: %v", got)
			}
		})
	}
}

func appendTestFile(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// Test variants carry a " [pkg.test]" suffix on the importer and on its
// imports; both are the package for the check. The tree's own build is
// read too: a package only a subpackage of the tree pulls in can import
// the tree's root.
func TestUnmountImportersReadsTestVariants(t *testing.T) {
	old := goListDeps
	t.Cleanup(func() { goListDeps = old })
	src := project.GofastrModule + "/core/yaml"
	goListDeps = func(_ context.Context, _, pattern string, tests bool) ([]byte, error) {
		if pattern != "./..." || !tests {
			if pattern != src+"/..." || tests {
				t.Errorf("listed %q, tests=%v", pattern, tests)
			}
			return []byte(src + "/sub\t" + project.GofastrModule + "/core/engine\n" + project.GofastrModule + "/core/engine\t" + src + "\n"), nil
		}
		return []byte(strings.Join([]string{
			"example.com/app\t" + src,
			"example.com/app_test [example.com/app.test]\t" + src,
			"example.com/app.test\t" + src,
			"example.com/plugin\t" + src,
			src + "\tfmt",
			project.GofastrModule + "/core/settings [example.com/app.test]\t" + src + " [example.com/app.test]",
			project.GofastrModule + "/core/yaml/sub\t" + src,
			project.GofastrModule + "/core/yamlish\tfmt",
		}, "\n")), nil
	}
	got, err := unmountImporters(context.Background(), ".", "example.com/app", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got[project.GofastrModule+"/core/settings"]) != 1 || len(got["example.com/plugin"]) != 1 || len(got[project.GofastrModule+"/core/engine"]) != 1 {
		t.Fatalf("importers = %v, want core/settings, example.com/plugin and core/engine", got)
	}
}

// Remount refuses to bring back a framework package that imports another
// copy the app holds.
func TestCheckRemountImporters(t *testing.T) {
	old := goListDeps
	t.Cleanup(func() { goListDeps = old })
	webhook, queue := project.GofastrModule+"/battery/webhook", project.GofastrModule+"/battery/queue"
	goListDeps = func(_ context.Context, _, pattern string, _ bool) ([]byte, error) {
		return []byte(webhook + "\t" + queue + "\tfmt\n" + queue + "\tfmt\n"), nil
	}
	hook := settings.Unmounted{Source: webhook, Path: "unmounted/battery/webhook"}
	q := settings.Unmounted{Source: queue, Path: "unmounted/battery/queue"}
	err := checkRemountImporters(context.Background(), ".", "example.com/app", hook, []settings.Unmounted{hook, q})
	if err == nil || !strings.Contains(err.Error(), webhook+" imports "+queue+", unmounted at unmounted/battery/queue") {
		t.Errorf("err = %v", err)
	}
	if err := checkRemountImporters(context.Background(), ".", "example.com/app", q, []settings.Unmounted{hook, q}); err != nil {
		t.Errorf("queue imports nothing unmounted: %v", err)
	}
}

func TestParseUnmountArgs(t *testing.T) {
	if pkg, out, dry, _, ok := parseUnmountArgs("unmount", []string{"core/yaml", "--out", "x", "--dry-run"}); !ok || pkg != "core/yaml" || out != "x" || !dry {
		t.Errorf("unmount args: %q %q %v %v", pkg, out, dry, ok)
	}
	if _, _, _, force, ok := parseUnmountArgs("remount", []string{"core/yaml", "--force"}); !ok || !force {
		t.Errorf("remount --force: %v %v", force, ok)
	}
	for _, tc := range []struct {
		cmd  string
		args []string
	}{
		{"unmount", []string{"--force", "core/yaml"}},
		{"remount", []string{"--out=x", "core/yaml"}},
		{"remount", nil},
		{"unmount", []string{"a", "b"}},
		{"unmount", []string{"--dry-run"}},
	} {
		clitest.CovT_capStdout(t, func() {
			if _, _, _, _, ok := parseUnmountArgs(tc.cmd, tc.args); ok {
				t.Errorf("%s %v accepted", tc.cmd, tc.args)
			}
		})
	}
}

// writeUnmountModule writes a temp app module that builds against this
// checkout, with main.go as given.
func writeUnmountModule(t *testing.T, mainGo string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	version, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+version+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), mainGo)
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	runGo(t, dir, "mod", "tidy")
	clitest.CovT_chdir(t, dir)
	return dir
}

func runGo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runUnmountCmd(t *testing.T, run func([]string), args ...string) (string, int) {
	t.Helper()
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { run(args) })
	})
	return out, code
}

const unmountMainGo = `package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/unmounted"
)

func main() {
	fmt.Println(textsafe.IsUnsafe('\u202e'))
	for _, c := range unmounted.List() {
		fmt.Println(c.Source, c.Version, c.Path)
	}
}
`

// The whole round trip on a real module: unmount copies the package,
// rewires main.go, records the copy and the binary reports it; remount
// refuses the edited copy, and --force goes back upstream.
func TestUnmountRemountRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	dir := writeUnmountModule(t, unmountMainGo)
	appgen.UnmountNow = func() time.Time { return time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { appgen.UnmountNow = time.Now })

	out, code := runUnmountCmd(t, RunRemount, "core/textsafe")
	if code != 1 || !strings.Contains(out, "is not unmounted here") {
		t.Fatalf("remount before unmount: exit %d\n%s", code, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "taken", "keep.txt"), "mine\n")
	out, code = runUnmountCmd(t, RunUnmount, "core/textsafe", "--out=taken")
	if code != 1 || !strings.Contains(out, "already exists and is not empty") {
		t.Fatalf("unmount into an occupied --out: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, RunUnmount, "core/textsafe")
	if code != -1 {
		t.Fatalf("unmount exited %d:\n%s", code, out)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), `"example.com/app/unmounted/core/textsafe"`) {
		t.Fatalf("main.go not rewired:\n%s", main)
	}
	f, err := settings.Load(filepath.Join(dir, settings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.Unmounted()
	want := settings.Unmounted{Source: project.GofastrModule + "/core/textsafe", Version: "dev", Path: "unmounted/core/textsafe", Date: "2026-10-19"}
	if err != nil || len(entries) != 1 || entries[0] != want {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted", "core", "textsafe", "textsafe_test.go")); !os.IsNotExist(err) {
		t.Errorf("a test file was copied: %v", err)
	}
	ran := runGo(t, dir, "run", ".")
	if !strings.Contains(ran, "true\n"+project.GofastrModule+"/core/textsafe dev example.com/app/unmounted/core/textsafe") {
		t.Errorf("the binary does not report the copy:\n%s", ran)
	}
	if out, _ := runUnmountCmd(t, RunUnmount); !strings.Contains(out, project.GofastrModule+"/core/textsafe dev -> unmounted/core/textsafe (2026-10-19)") {
		t.Errorf("list:\n%s", out)
	}

	appendTestFile(t, filepath.Join(dir, "unmounted", "core", "textsafe", "textsafe.go"), "\n// mine\n")
	out, code = runUnmountCmd(t, RunRemount, "core/textsafe", "--dry-run")
	if code != -1 || !strings.Contains(out, "Would delete unmounted/core/textsafe") || !strings.Contains(out, "textsafe.go (edited)") {
		t.Fatalf("remount --dry-run of an edited copy: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, RunRemount, "core/textsafe")
	if code != 1 || !strings.Contains(out, "textsafe.go (edited)") {
		t.Fatalf("remount of an edited copy: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, RunRemount, "unmounted/core/textsafe", "--force")
	if code != -1 {
		t.Fatalf("remount --force exited %d:\n%s", code, out)
	}
	main, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), `"`+project.GofastrModule+`/core/textsafe"`) {
		t.Errorf("main.go not rewired back:\n%s", main)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		t.Errorf("the copy or its emptied parents are still there: %v", err)
	}
	settingsBody, _ := os.ReadFile(filepath.Join(dir, settings.FileName))
	if strings.Contains(string(settingsBody), "unmounted:") {
		t.Errorf("the entry is still recorded:\n%s", settingsBody)
	}
	runGo(t, dir, "build", "./...")
}

// A package a package outside the module imports is refused, and
// the refusal names the importer; a package that imports gofastr
// internals is refused before the build is listed.
func TestUnmountRefusals(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	dir := writeUnmountModule(t, `package main

import _ "github.com/DonaldMurillo/gofastr/core/settings"

func main() {}
`)
	for _, tc := range []struct{ pkg, want string }{
		{"core/yaml", project.GofastrModule + "/core/settings imports " + project.GofastrModule + "/core/yaml"},
		{"core/upload", "imports " + project.GofastrModule + "/internal/fileperm"},
	} {
		out, code := runUnmountCmd(t, RunUnmount, tc.pkg)
		if code != 1 || !strings.Contains(out, tc.want) {
			t.Errorf("%s: exit %d, want a refusal naming %q:\n%s", tc.pkg, code, tc.want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		t.Errorf("a refused unmount wrote files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, settings.FileName)); !os.IsNotExist(err) {
		t.Errorf("a refused unmount wrote settings: %v", err)
	}
}

// A directory the go tool skips stays out of the copy unless a non-test
// file beside it embeds from it.
func TestReadUnmountTreeKeepsEmbeddedDirs(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "framework", "demo")
	for name, body := range map[string]string{
		"demo.go":            "package demo\n\nimport _ \"embed\"\n\n//go:embed testdata/axe.js\nvar axe string\n",
		"demo_test.go":       "package demo\n\n//go:embed _fixtures\nvar fixtures string\n",
		"testdata/axe.js":    "axe\n",
		"_fixtures/a.txt":    "a\n",
		".hidden/b.txt":      "b\n",
		"sub/testdata/c.txt": "c\n",
		"sub/sub.go":         "package sub\n",
	} {
		p := filepath.Join(pkg, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, p, body)
	}
	tree, err := readUnmountTree(dir, project.GofastrModule+"/framework/demo", "v0.91.0")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range tree.Files {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, " "); got != "demo.go sub/sub.go testdata/axe.js" {
		t.Errorf("copied %s", got)
	}
}

func TestCheckUnmountRegistry(t *testing.T) {
	old := t.TempDir()
	if err := checkUnmountRegistry(old, "v0.91.0"); err == nil || !strings.Contains(err.Error(), "upgrade gofastr first") {
		t.Errorf("a gofastr without core/unmounted: err = %v", err)
	}
	if err := checkUnmountRegistry(clitest.RepoRootDir(t), "dev"); err != nil {
		t.Errorf("this checkout: %v", err)
	}
}

// A gofastr that predates core/unmounted is refused before anything is
// written: the copy's hook would import a package that is not there.
func TestUnmountRefusesOldGofastr(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	version, err := clitest.RepoGoVersion(clitest.RepoRootDir(t))
	if err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(fake, "go.mod"), "module "+project.GofastrModule+"\n\ngo "+version+"\n")
	if err := os.MkdirAll(filepath.Join(fake, "core", "tiny"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(fake, "core", "tiny", "tiny.go"), "package tiny\n\nfunc F() int { return 1 }\n")
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+version+"\n\nrequire "+project.GofastrModule+" v0.0.0\n\nreplace "+project.GofastrModule+" => "+fake+"\n")
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/tiny\"\n\nfunc main() { _ = tiny.F() }\n")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	clitest.CovT_chdir(t, dir)
	out, code := runUnmountCmd(t, RunUnmount, "core/tiny")
	if code != 1 || !strings.Contains(out, "upgrade gofastr first") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		t.Errorf("a refused unmount wrote files: %v", err)
	}
}

// A copy deleted by hand has nothing left to lose: remount, without
// --force, rewires the imports back and drops the entry verify warns about.
func TestRemountDeletedCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	dir := writeUnmountModule(t, unmountMainGo)
	if out, code := runUnmountCmd(t, RunUnmount, "core/textsafe"); code != -1 {
		t.Fatalf("unmount exited %d:\n%s", code, out)
	}
	if err := os.RemoveAll(filepath.Join(dir, "unmounted")); err != nil {
		t.Fatal(err)
	}
	out, code := runUnmountCmd(t, RunRemount, "core/textsafe")
	if code != -1 || !strings.Contains(out, "gone already") {
		t.Fatalf("remount of a deleted copy: exit %d\n%s", code, out)
	}
	if main, _ := os.ReadFile(filepath.Join(dir, "main.go")); !strings.Contains(string(main), `"`+project.GofastrModule+`/core/textsafe"`) {
		t.Errorf("main.go not rewired back:\n%s", main)
	}
	if _, err := os.Stat(filepath.Join(dir, settings.FileName)); !os.IsNotExist(err) {
		t.Errorf("the entry is still recorded: %v", err)
	}
}

// The count a failed rewire returns covers the file that failed: a write
// can fail after its truncate, so the undo must restore that file too.
func TestApplyImportRewireCountsFailedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits")
	}
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "a.go"), "package a\n")
	clitest.WriteTestFile(t, filepath.Join(dir, "b.go"), "package a\n")
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	plan := []importRewire{
		{name: "a.go", mode: 0o644, old: "package a\n", new: "package a // new\n"},
		{name: "b.go", mode: 0o644, old: "package a\n", new: "package a // new\n"},
	}
	n, err := applyImportRewire(dir, plan, false)
	if err == nil {
		t.Fatal("a read-only file was replaced")
	}
	if n != 2 {
		t.Errorf("n = %d, want 2: the undo would skip the failed file", n)
	}
}

func TestFindUnmountedAmbiguousSource(t *testing.T) {
	src := appgen.CanonicalPackageImport + "siteheader"
	entries := []settings.Unmounted{{Source: src, Path: "alpha/siteheader"}, {Source: src, Path: "beta/siteheader"}}
	if _, err := findUnmounted(src, "/m", "/m", entries); err == nil || !strings.Contains(err.Error(), "alpha/siteheader, beta/siteheader") {
		t.Errorf("by source: err = %v", err)
	}
	if i, err := findUnmounted("beta/siteheader", "/m", "/m", entries); err != nil || i != 1 {
		t.Errorf("by path: %d, %v", i, err)
	}
}

// The remount check runs on the real command: webhook imports queue, so
// with both unmounted, remounting webhook alone is refused, --force too.
// Unmounting copies both packages' source out of the module.
//
//gofastr:test-reads battery/webhook/
//gofastr:test-reads battery/queue/
func TestRemountRefusesImporterOfCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	writeUnmountModule(t, "package main\n\nimport _ \"github.com/DonaldMurillo/gofastr/battery/webhook\"\n\nfunc main() {}\n")
	for _, pkg := range []string{"battery/webhook", "battery/queue"} {
		if out, code := runUnmountCmd(t, RunUnmount, pkg); code != -1 {
			t.Fatalf("unmount %s exited %d:\n%s", pkg, code, out)
		}
	}
	out, code := runUnmountCmd(t, RunRemount, "battery/webhook", "--force")
	if code != 1 || !strings.Contains(out, project.GofastrModule+"/battery/webhook imports "+project.GofastrModule+"/battery/queue") {
		t.Fatalf("remount webhook: exit %d\n%s", code, out)
	}
}

// Remount refuses when the gofastr go.mod requires no longer has the
// package: the rewired imports would point at nothing.
func TestRemountRefusesMissingUpstream(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestModule(t, dir)
	clitest.CovT_chdir(t, dir)
	clitest.WriteUnmountedSettings(t, dir, settings.Unmounted{Source: project.GofastrModule + "/core/gone", Version: "v0.91.0", Path: "unmounted/core/gone", Date: "2026-10-19"})
	old := goListModule
	t.Cleanup(func() { goListModule = old })
	upstream := t.TempDir()
	goListModule = func(context.Context, string) ([]byte, error) {
		return []byte(`{"Version":"v0.93.0","Dir":` + strconv.Quote(upstream) + `}`), nil
	}
	out, code := runUnmountCmd(t, RunRemount, "core/gone")
	if code != 1 || !strings.Contains(out, "gofastr v0.93.0 has no package "+project.GofastrModule+"/core/gone") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// The rewire truncated each source file in place, so a crash mid-plan
// left it empty with the undo never run; each is renamed over whole.
func TestApplyImportRewireReplacesWhole(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "a.go"), "package a\n")
	old := heldRewire(t, filepath.Join(dir, "a.go"))
	plan := []importRewire{{name: "a.go", mode: 0o644, old: "package a\n", new: "package a // new\n"}}
	if _, err := applyImportRewire(dir, plan, false); err != nil {
		t.Fatal(err)
	}
	if got := old(); got != "package a\n" {
		t.Fatalf("the old handle reads %q: rewritten in place", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.go")); string(got) != "package a // new\n" {
		t.Fatalf("a.go = %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %d entries, want a.go alone", len(entries))
	}
	// A rename that fails (here over a non-empty directory) takes its
	// temporary file with it.
	if err := os.MkdirAll(filepath.Join(dir, "d.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "d.go", "x"), "")
	plan = []importRewire{{name: "d.go", mode: 0o644, new: "package a\n"}}
	if _, err := applyImportRewire(dir, plan, false); err == nil {
		t.Fatal("rename over a directory succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("left %d entries, want a.go and d.go: the temporary file stayed", len(entries))
	}
}

// heldRewire opens path and returns a reader of what that handle sees: a
// file renamed over path leaves it reading the old content, one
// truncated in place leaves it reading the new.
func heldRewire(t *testing.T, path string) func() string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return func() string {
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}
