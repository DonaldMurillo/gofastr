package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestUnmountSourcePathForms(t *testing.T) {
	for arg, want := range map[string]string{
		"battery/auth":                     gofastrModule + "/battery/auth",
		gofastrModule + "/battery/auth":    gofastrModule + "/battery/auth",
		gofastrModule + "/battery/auth/":   gofastrModule + "/battery/auth",
		"framework/ui/theme":               gofastrModule + "/framework/ui/theme",
		gofastrModule + "/core/textsafe/x": gofastrModule + "/core/textsafe/x",
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

// A sibling whose name starts with the tree's is not in the tree.
func TestRewriteImportPrefixKeepsSiblings(t *testing.T) {
	src := "package x\n\nimport (\n\t\"" + gofastrModule + "/battery/auth\"\n\t\"" + gofastrModule + "/battery/auth/oidc\"\n\t\"" + gofastrModule + "/battery/authz\"\n)\n\n// \"" + gofastrModule + "/battery/auth\" in a comment\n"
	got, changed, err := rewriteImportPrefix(src, gofastrModule+"/battery/auth", "example.com/app/auth")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	for _, want := range []string{`"example.com/app/auth"`, `"example.com/app/auth/oidc"`, `"` + gofastrModule + `/battery/authz"`, `// "` + gofastrModule + `/battery/auth" in a comment`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}

func testUnmountTree() unmountTree {
	src := gofastrModule + "/battery/demo"
	return unmountTree{source: src, version: "v0.91.0", files: []packageFile{
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
	files, err := buildUnmountCopy(tree, "example.com/app/unmounted/battery/demo")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[f.name] = f.content
	}
	for _, name := range []string{"demo.go", "sub/sub.go", unmountHookFile, "sub/" + unmountHookFile} {
		m, _, ok := codegen.ReadMarker(name, byName[name])
		if !ok || m.Generator != unmountGenerator || m.Source != tree.source || m.Version != "v0.91.0" {
			t.Errorf("%s marker = %+v ok=%v", name, m, ok)
		}
	}
	if !strings.Contains(byName["demo.go"], `"example.com/app/unmounted/battery/demo/sub"`) {
		t.Errorf("self-import not rewritten:\n%s", byName["demo.go"])
	}
	if byName["static/app.css"] != tree.files[3].Content {
		t.Errorf("an asset was changed: %q", byName["static/app.css"])
	}
	root := byName[unmountHookFile]
	if !strings.HasPrefix(strings.SplitN(root, "\n\n", 2)[1], "package demo\n") {
		t.Errorf("root hook names the wrong package (the test file and the ignored program say otherwise):\n%s", root)
	}
	if !strings.Contains(byName["sub/"+unmountHookFile], "package sub\n") {
		t.Errorf("sub hook:\n%s", byName["sub/"+unmountHookFile])
	}
	listed := readUnmountFiles(root)
	if len(listed) != len(files)-1 || listed["static/app.css"] != codegen.Sum(tree.files[3].Content) {
		t.Errorf("listed = %v", listed)
	}
	if _, ok := listed[unmountHookFile]; ok {
		t.Error("the root hook lists itself")
	}
}

func writeUnmountCopy(t *testing.T, dir string) {
	t.Helper()
	files, err := buildUnmountCopy(testUnmountTree(), "example.com/app/unmounted/battery/demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(dir, "unmounted", "battery", "demo", filepath.FromSlash(f.name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, p, f.content)
	}
}

func TestUnmountCopyChanges(t *testing.T) {
	entry := settings.Unmounted{Source: gofastrModule + "/battery/demo", Version: "v0.91.0", Path: "unmounted/battery/demo", Date: "2026-10-19"}
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
			writeTestFile(t, filepath.Join(d, copyDir, "sub", "extra.go"), "package sub\n")
		}, []string{"sub/extra.go (added)"}},
		{"deleted", func(d string) {
			if err := os.Remove(filepath.Join(d, copyDir, "sub", "sub.go")); err != nil {
				t.Fatal(err)
			}
		}, []string{"sub/sub.go (deleted)"}},
		{"hook edited", func(d string) {
			appendTestFile(t, filepath.Join(d, copyDir, unmountHookFile), "// mine\n")
		}, []string{unmountHookFile + " (edited)", "demo.go (added)"}},
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
	src := gofastrModule + "/core/yaml"
	goListDeps = func(_ context.Context, _, pattern string, tests bool) ([]byte, error) {
		if pattern != "./..." || !tests {
			if pattern != src+"/..." || tests {
				t.Errorf("listed %q, tests=%v", pattern, tests)
			}
			return []byte(src + "/sub\t" + gofastrModule + "/core/engine\n" + gofastrModule + "/core/engine\t" + src + "\n"), nil
		}
		return []byte(strings.Join([]string{
			"example.com/app\t" + src,
			"example.com/app_test [example.com/app.test]\t" + src,
			"example.com/app.test\t" + src,
			"example.com/plugin\t" + src,
			src + "\tfmt",
			gofastrModule + "/core/settings [example.com/app.test]\t" + src + " [example.com/app.test]",
			gofastrModule + "/core/yaml/sub\t" + src,
			gofastrModule + "/core/yamlish\tfmt",
		}, "\n")), nil
	}
	got, err := unmountImporters(context.Background(), ".", "example.com/app", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got[gofastrModule+"/core/settings"]) != 1 || len(got["example.com/plugin"]) != 1 || len(got[gofastrModule+"/core/engine"]) != 1 {
		t.Fatalf("importers = %v, want core/settings, example.com/plugin and core/engine", got)
	}
}

// Remount refuses to bring back a framework package that imports another
// copy the app holds.
func TestCheckRemountImporters(t *testing.T) {
	old := goListDeps
	t.Cleanup(func() { goListDeps = old })
	webhook, queue := gofastrModule+"/battery/webhook", gofastrModule+"/battery/queue"
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

func TestCheckUnmountOverlap(t *testing.T) {
	entries := []settings.Unmounted{{Source: gofastrModule + "/battery/auth", Path: "unmounted/battery/auth"}}
	for _, tc := range []struct {
		source, path string
		rewire       bool
		want         string
	}{
		{gofastrModule + "/battery/auth", "x", true, "already unmounted"},
		{gofastrModule + "/battery/auth/oidc", "x", true, "part of the unmounted copy"},
		{gofastrModule + "/battery", "x", true, "remount it first"},
		{gofastrModule + "/core/yaml", "unmounted/battery/auth/yaml", true, "overlaps"},
		{gofastrModule + "/battery/authz", "unmounted/battery/authz", true, ""},
		// generate package: a second copy of a source at its own path.
		{gofastrModule + "/battery/auth", "beta/auth", false, ""},
		{gofastrModule + "/battery/auth", "unmounted/battery/auth/x", false, "overlaps"},
	} {
		err := checkUnmountOverlap(settings.Unmounted{Source: tc.source, Path: tc.path}, entries, tc.rewire)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s at %s: err = %v, want %q", tc.source, tc.path, err, tc.want)
		}
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
		covT_capStdout(t, func() {
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
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	version, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+version+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "main.go"), mainGo)
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	runGo(t, dir, "mod", "tidy")
	covT_chdir(t, dir)
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
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { run(args) })
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
	unmountNow = func() time.Time { return time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { unmountNow = time.Now })

	out, code := runUnmountCmd(t, runRemount, "core/textsafe")
	if code != 1 || !strings.Contains(out, "is not unmounted here") {
		t.Fatalf("remount before unmount: exit %d\n%s", code, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "taken", "keep.txt"), "mine\n")
	out, code = runUnmountCmd(t, runUnmount, "core/textsafe", "--out=taken")
	if code != 1 || !strings.Contains(out, "already exists and is not empty") {
		t.Fatalf("unmount into an occupied --out: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, runUnmount, "core/textsafe")
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
	want := settings.Unmounted{Source: gofastrModule + "/core/textsafe", Version: "dev", Path: "unmounted/core/textsafe", Date: "2026-10-19"}
	if err != nil || len(entries) != 1 || entries[0] != want {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted", "core", "textsafe", "textsafe_test.go")); !os.IsNotExist(err) {
		t.Errorf("a test file was copied: %v", err)
	}
	ran := runGo(t, dir, "run", ".")
	if !strings.Contains(ran, "true\n"+gofastrModule+"/core/textsafe dev example.com/app/unmounted/core/textsafe") {
		t.Errorf("the binary does not report the copy:\n%s", ran)
	}
	if out, _ := runUnmountCmd(t, runUnmount); !strings.Contains(out, gofastrModule+"/core/textsafe dev -> unmounted/core/textsafe (2026-10-19)") {
		t.Errorf("list:\n%s", out)
	}

	appendTestFile(t, filepath.Join(dir, "unmounted", "core", "textsafe", "textsafe.go"), "\n// mine\n")
	out, code = runUnmountCmd(t, runRemount, "core/textsafe", "--dry-run")
	if code != -1 || !strings.Contains(out, "Would delete unmounted/core/textsafe") || !strings.Contains(out, "textsafe.go (edited)") {
		t.Fatalf("remount --dry-run of an edited copy: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, runRemount, "core/textsafe")
	if code != 1 || !strings.Contains(out, "textsafe.go (edited)") {
		t.Fatalf("remount of an edited copy: exit %d\n%s", code, out)
	}
	out, code = runUnmountCmd(t, runRemount, "unmounted/core/textsafe", "--force")
	if code != -1 {
		t.Fatalf("remount --force exited %d:\n%s", code, out)
	}
	main, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), `"`+gofastrModule+`/core/textsafe"`) {
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
		{"core/yaml", gofastrModule + "/core/settings imports " + gofastrModule + "/core/yaml"},
		{"core/upload", "imports " + gofastrModule + "/internal/fileperm"},
	} {
		out, code := runUnmountCmd(t, runUnmount, tc.pkg)
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

func writeUnmountedSettings(t *testing.T, dir string, entries ...settings.Unmounted) {
	t.Helper()
	if err := settings.SetUnmounted(filepath.Join(dir, settings.FileName), entries); err != nil {
		t.Fatal(err)
	}
}

// verify lists each copy, warns for one older than go.mod's release, for
// an entry with no files, and for a marked copy the settings file lost.
func TestReportUnmountedWarnings(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.27\n\nrequire "+gofastrModule+" v0.92.0\n")
	old := settings.Unmounted{Source: gofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"}
	gone := settings.Unmounted{Source: gofastrModule + "/core/textsafe", Version: "v0.92.0", Path: "unmounted/core/textsafe", Date: "2026-10-19"}
	writeUnmountedSettings(t, dir, old, gone)
	if err := os.MkdirAll(filepath.Join(dir, "unmounted", "core", "yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "unmounted", "core", "yaml", "yaml.go"), "package yaml\n")
	stray, err := codegen.Stamp("x.go", "package x\n", codegen.Marker{Generator: unmountGenerator, Kind: codegen.KindScaffold, Version: "v0.90.0", Source: gofastrModule + "/battery/auth"})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "x.go"), stray)

	var notes, warns []string
	marked, err := scanProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	reportUnmounted(dir, marked,
		func(f string, a ...any) { notes = append(notes, fmt.Sprintf(f, a...)) },
		func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) })
	all := strings.Join(warns, "\n")
	for _, want := range []string{
		"core/yaml was copied at v0.91.0 and go.mod requires gofastr v0.92.0",
		unmountedCompareURL("v0.91.0", "v0.92.0"),
		"lists " + gofastrModule + "/core/textsafe at unmounted/core/textsafe, but that directory holds no files",
		"x.go is marked as an unmounted copy of " + gofastrModule + "/battery/auth",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings miss %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "textsafe was copied") {
		t.Errorf("a current copy is reported behind:\n%s", all)
	}
	if len(notes) != 2 || !strings.Contains(notes[0], "core/textsafe v0.92.0 at unmounted/core/textsafe (unmounted 2026-10-19)") {
		t.Errorf("notes = %v", notes)
	}
}

func TestUpgradeListsUnmountedDiffs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n")
	writeUnmountedSettings(t, dir,
		settings.Unmounted{Source: gofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"},
		settings.Unmounted{Source: gofastrModule + "/core/textsafe", Version: "dev", Path: "unmounted/core/textsafe", Date: "2026-10-19"})
	out := covT_capStdout(t, func() { printUnmountedForUpgrade(dir, "v0.93.0") })
	for _, want := range []string{"upgrade never changes them", unmountedCompareURL("v0.91.0", "v0.93.0"), "copied from a checkout, not a release: compare it with v0.93.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// A failed write takes back the directories it created inside the
// target too, not only the target itself.
func TestPackageCopyRollbackRemovesSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	packageWriteFiles = func(files *codegen.FileSet, opts codegen.WriteOptions) error {
		if err := codegen.WriteFiles(files, opts); err != nil {
			return err
		}
		return errors.New("disk full")
	}
	t.Cleanup(func() { packageWriteFiles = codegen.WriteFiles })
	wd, _ := os.Getwd()
	tgt, err := resolvePackageTarget("demo", "unmounted/demo", wd)
	if err != nil {
		t.Fatal(err)
	}
	files := []generatedFile{{name: "demo.go", content: "package demo\n"}, {name: "a/b/c.go", content: "package b\n"}, {name: "a/d.go", content: "package a\n"}}
	if _, err := writePackageCopy(tgt, files); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(filepath.Join(dir, "unmounted", "demo"))
		t.Errorf("the failed copy left %v behind: %v", entries, err)
	}
}

// The undo a successful write returns, used when a later step fails,
// removes the parents the write created and keeps a target that existed
// empty before.
func TestPackageCopyUndo(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	wd, _ := os.Getwd()
	files := []generatedFile{{name: "demo.go", content: "package demo\n"}, {name: "a/d.go", content: "package a\n"}}

	tgt, err := resolvePackageTarget("demo", "unmounted/battery/demo", wd)
	if err != nil {
		t.Fatal(err)
	}
	undo, err := writePackageCopy(tgt, files)
	if err != nil {
		t.Fatal(err)
	}
	undo()
	if _, err := os.Stat(filepath.Join(dir, "unmounted")); !os.IsNotExist(err) {
		t.Errorf("undo left the parents it created: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	tgt, err = resolvePackageTarget("demo", "mine", wd)
	if err != nil {
		t.Fatal(err)
	}
	if undo, err = writePackageCopy(tgt, files); err != nil {
		t.Fatal(err)
	}
	undo()
	if entries, err := os.ReadDir(filepath.Join(dir, "mine")); err != nil || len(entries) != 0 {
		t.Errorf("undo removed the target that existed, or left files: %v %v", entries, err)
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
		writeTestFile(t, p, body)
	}
	tree, err := readUnmountTree(dir, gofastrModule+"/framework/demo", "v0.91.0")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range tree.files {
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
	if err := checkUnmountRegistry(repoRootDir(t), "dev"); err != nil {
		t.Errorf("this checkout: %v", err)
	}
}

// A gofastr that predates core/unmounted is refused before anything is
// written: the copy's hook would import a package that is not there.
func TestUnmountRefusesOldGofastr(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	version, err := repoGoVersion(repoRootDir(t))
	if err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	writeTestFile(t, filepath.Join(fake, "go.mod"), "module "+gofastrModule+"\n\ngo "+version+"\n")
	if err := os.MkdirAll(filepath.Join(fake, "core", "tiny"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(fake, "core", "tiny", "tiny.go"), "package tiny\n\nfunc F() int { return 1 }\n")
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+version+"\n\nrequire "+gofastrModule+" v0.0.0\n\nreplace "+gofastrModule+" => "+fake+"\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/tiny\"\n\nfunc main() { _ = tiny.F() }\n")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	covT_chdir(t, dir)
	out, code := runUnmountCmd(t, runUnmount, "core/tiny")
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
	if out, code := runUnmountCmd(t, runUnmount, "core/textsafe"); code != -1 {
		t.Fatalf("unmount exited %d:\n%s", code, out)
	}
	if err := os.RemoveAll(filepath.Join(dir, "unmounted")); err != nil {
		t.Fatal(err)
	}
	out, code := runUnmountCmd(t, runRemount, "core/textsafe")
	if code != -1 || !strings.Contains(out, "gone already") {
		t.Fatalf("remount of a deleted copy: exit %d\n%s", code, out)
	}
	if main, _ := os.ReadFile(filepath.Join(dir, "main.go")); !strings.Contains(string(main), `"`+gofastrModule+`/core/textsafe"`) {
		t.Errorf("main.go not rewired back:\n%s", main)
	}
	if _, err := os.Stat(filepath.Join(dir, settings.FileName)); !os.IsNotExist(err) {
		t.Errorf("the entry is still recorded: %v", err)
	}
}

// The count a failed rewire returns covers the file that failed: a write
// can fail after its truncate, so the undo must restore that file too.
func TestApplyImportRewireCountsFailedFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.go"), "package a\n")
	writeTestFile(t, filepath.Join(dir, "b.go"), "package a\n")
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	plan := []importRewire{
		{name: "a.go", mode: 0o644, old: "package a\n", new: "package a // new\n"},
		{name: "b.go", mode: 0o644, old: "package a\n", new: "package a // new\n"},
	}
	n, err := applyImportRewire(dir, plan, false)
	if err == nil {
		t.Skip("read-only file was written (running as root?)")
	}
	if n != 2 {
		t.Errorf("n = %d, want 2: the undo would skip the failed file", n)
	}
}

func TestFindUnmountedAmbiguousSource(t *testing.T) {
	src := canonicalPackageImport + "siteheader"
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
func TestRemountRefusesImporterOfCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain in a temp module")
	}
	writeUnmountModule(t, "package main\n\nimport _ \"github.com/DonaldMurillo/gofastr/battery/webhook\"\n\nfunc main() {}\n")
	for _, pkg := range []string{"battery/webhook", "battery/queue"} {
		if out, code := runUnmountCmd(t, runUnmount, pkg); code != -1 {
			t.Fatalf("unmount %s exited %d:\n%s", pkg, code, out)
		}
	}
	out, code := runUnmountCmd(t, runRemount, "battery/webhook", "--force")
	if code != 1 || !strings.Contains(out, gofastrModule+"/battery/webhook imports "+gofastrModule+"/battery/queue") {
		t.Fatalf("remount webhook: exit %d\n%s", code, out)
	}
}

// Without a readable go.mod verify cannot compare the copies with the
// release, and says so instead of going quiet.
func TestReportUnmountedNoGoMod(t *testing.T) {
	dir := t.TempDir()
	writeUnmountedSettings(t, dir, settings.Unmounted{Source: gofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"})
	var warns []string
	reportUnmounted(dir, nil, func(string, ...any) {}, func(f string, a ...any) { warns = append(warns, fmt.Sprintf(f, a...)) })
	if all := strings.Join(warns, "\n"); !strings.Contains(all, "cannot tell whether the unmounted copies are behind") {
		t.Errorf("warnings:\n%s", all)
	}
}

// Remount refuses when the gofastr go.mod requires no longer has the
// package: the rewired imports would point at nothing.
func TestRemountRefusesMissingUpstream(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	writeUnmountedSettings(t, dir, settings.Unmounted{Source: gofastrModule + "/core/gone", Version: "v0.91.0", Path: "unmounted/core/gone", Date: "2026-10-19"})
	old := goListModule
	t.Cleanup(func() { goListModule = old })
	upstream := t.TempDir()
	goListModule = func(context.Context, string) ([]byte, error) {
		return []byte(`{"Version":"v0.93.0","Dir":` + strconv.Quote(upstream) + `}`), nil
	}
	out, code := runUnmountCmd(t, runRemount, "core/gone")
	if code != 1 || !strings.Contains(out, "gofastr v0.93.0 has no package "+gofastrModule+"/core/gone") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
