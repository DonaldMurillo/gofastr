package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The list-mode output comes from the embedded package doc comments,
// not a hand-maintained table: every canonical package appears with a
// one-line summary, so a new package under packages/ is discoverable
// the moment it lands.
func TestGeneratePackageListsAllPackages(t *testing.T) {
	out := covT_capStdout(t, func() {
		runGeneratePackage(nil)
	})
	for _, want := range []string{
		"docpage",
		"sitefooter",
		"siteheader",
		// One summary fragment per package, proving the doc comment
		// was parsed rather than the names echoed alone.
		"top bar",
		"colophon",
		"help/docs page",
		"gofastr generate package <name>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

// An unknown name is refused with the valid ones listed, exit 1.
func TestGeneratePackageUnknownNameRefused(t *testing.T) {
	var out string
	code := -1
	out = covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runGeneratePackage([]string{"banner"})
		})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{"unknown package", "banner", "siteheader", "sitefooter", "docpage"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal missing %q:\n%s", want, out)
		}
	}
}

// The copied code imports itself by module path, so the command refuses
// outside a Go module instead of writing a package that cannot build.
func TestGeneratePackageNeedsGoMod(t *testing.T) {
	covT_chdir(t, t.TempDir())
	var out string
	code := -1
	out = covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runGeneratePackage([]string{"siteheader"})
		})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "no enclosing go.mod") {
		t.Errorf("refusal does not name the missing go.mod:\n%s", out)
	}
	if _, err := os.Stat("siteheader"); !os.IsNotExist(err) {
		t.Errorf("refused run still wrote the target: %v", err)
	}
}

// One-shot copy: an existing non-empty target is someone's code, so the
// copy refuses rather than merging into or overwriting it.
func TestGeneratePackageRefusesNonEmptyTarget(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "taken", "mine.go"), "package taken\n")
	covT_chdir(t, dir)

	var out string
	code := -1
	out = covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runGeneratePackage([]string{"siteheader", "--out=taken"})
		})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "already exists and is not empty") {
		t.Errorf("refusal does not say the target is occupied:\n%s", out)
	}
	kept, err := os.ReadFile(filepath.Join(dir, "taken", "mine.go"))
	if err != nil || string(kept) != "package taken\n" {
		t.Errorf("occupied target was touched: %q %v", kept, err)
	}
}

// The target's import path is spliced into Go source, so a directory
// name that is not a valid import path is refused before any write.
func TestGeneratePackageRefusesBadImport(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	for _, out := range []string{`my "pkg"`, "my pkg"} {
		code := -1
		msg := covT_capStdout(t, func() {
			code = covT_capExit(t, func() {
				runGeneratePackage([]string{"siteheader", "--out=" + out})
			})
		})
		if code != 1 {
			t.Fatalf("--out=%q: exit = %d, want 1\n%s", out, code, msg)
		}
		if _, err := os.Stat(filepath.Join(dir, out)); err == nil {
			t.Errorf("--out=%q: refused copy still wrote the directory", out)
		}
	}
}

// --dry-run prints the file list and writes nothing.
func TestGeneratePackageDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)

	out := covT_capStdout(t, func() {
		runGeneratePackage([]string{"docpage", "--dry-run"})
	})
	for _, want := range []string{"docpage.go", "docpage.style.css", "docpage_test.go", "Would write:"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run missing %q:\n%s", want, out)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "docpage")); err == nil && len(entries) > 0 {
		t.Errorf("dry run wrote %d file(s)", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "docpage")); !os.IsNotExist(err) {
		t.Errorf("dry run created the target directory: %v", err)
	}
}

// --dry-run must judge exactly the targets the real copy can write: a
// target that escapes the working directory but not the module (spelled
// with "..", or absolute) is approved by the dry run and then has to
// survive the real write from the same place, not fail with the writer's
// own jargon.
func TestGeneratePackageSubdirOutWorks(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	sub := filepath.Join(dir, "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, sub)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The absolute case spells the target the way a shell would (from the
	// resolved working directory), not through a symlinked alias of it.
	for _, tc := range []struct{ name, out string }{
		{"parent-relative", "../chrome/hdr"},
		{"absolute", filepath.Join(wd, "..", "chrome", "abs-hdr")},
	} {
		dry := covT_capStdout(t, func() {
			runGeneratePackage([]string{"docpage", "--out=" + tc.out, "--dry-run"})
		})
		if !strings.Contains(dry, "Would write:") || !strings.Contains(dry, tc.out+"/docpage.go") {
			t.Errorf("%s: dry run did not approve the in-module target:\n%s", tc.name, dry)
		}
		var code int
		out := covT_capStdout(t, func() {
			code = covT_capExit(t, func() {
				runGeneratePackage([]string{"docpage", "--out=" + tc.out})
			})
		})
		if code != -1 {
			t.Errorf("%s: real copy from a subdirectory exited %d:\n%s", tc.name, code, out)
		}
		if _, serr := os.Stat(filepath.Join(dir, "chrome", filepath.Base(tc.out), "docpage.go")); serr != nil {
			t.Errorf("%s: files did not land in the module: %v", tc.name, serr)
		}
	}
}

// A target outside the enclosing module is refused with the CLI's own
// message in BOTH modes — the dry run must not approve a write the copy
// would refuse.
func TestGeneratePackageDryRunRefusesOutside(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	sub := filepath.Join(dir, "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, sub)

	for _, args := range [][]string{
		{"siteheader", "--out=../.."},
		{"siteheader", "--out=../..", "--dry-run"},
	} {
		var code int
		out := covT_capStdout(t, func() {
			code = covT_capExit(t, func() {
				runGeneratePackage(args)
			})
		})
		if code != 1 {
			t.Errorf("%v: exit = %d, want 1\n%s", args, code, out)
		}
		if !strings.Contains(out, "sits outside the module") {
			t.Errorf("%v: refusal does not name the module fence:\n%s", args, out)
		}
		if strings.Contains(out, "Would write:") {
			t.Errorf("%v: dry run approved an out-of-module target:\n%s", args, out)
		}
	}
}

// The import path comes from the go.mod that encloses the TARGET: with a
// nested tools/go.mod, a copy into tools/ftr run from the outer root must
// import example.com/tools/ftr, not a path glued onto the outer module.
func TestGeneratePackageNestedModuleImport(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	tools := filepath.Join(dir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	version, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(tools, "go.mod"), "module example.com/tools\n\ngo "+version+"\n")
	covT_chdir(t, dir)

	out := covT_capStdout(t, func() {
		runGeneratePackage([]string{"sitefooter", "--out=tools/ftr"})
	})
	if !strings.Contains(out, "Copied sitefooter into tools/ftr") {
		t.Fatalf("copy into the nested module failed:\n%s", out)
	}
	test, err := os.ReadFile(filepath.Join(dir, "tools", "ftr", "sitefooter_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(test), `"example.com/tools/ftr"`) {
		t.Errorf("self-import not derived from the nested go.mod:\n%s", test)
	}
	for _, f := range packageCopyFiles(t, filepath.Join(dir, "tools", "ftr")) {
		if strings.Contains(f.content, "pkgcopy/tools") {
			t.Errorf("%s imports through the outer module", f.path)
		}
	}
}

// A file another process drops into the target between the emptiness
// check and the write is never overwritten: the copy refuses, and the
// rollback removes only the files this run wrote — the racer's file, and
// anything else this run did not create, is left untouched.
func TestGeneratePackageRacerRolledBack(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	sentinel := []byte("another process wrote here first\n")
	packageWriteFiles = func(files *codegen.FileSet, opts codegen.WriteOptions) error {
		all := files.All()
		target, err := filepath.Abs(opts.OutputRoot)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, all[len(all)-1].Path), sentinel, 0o644); err != nil {
			return err
		}
		return codegen.WriteFiles(files, opts)
	}
	t.Cleanup(func() { packageWriteFiles = codegen.WriteFiles })

	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runGeneratePackage([]string{"sitefooter"})
		})
	})
	if code != 1 {
		t.Fatalf("a racing file must fail the copy, exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Failed to write package files") {
		t.Errorf("failure does not name the write:\n%s", out)
	}
	if !strings.Contains(out, "Rolled back the partial copy") {
		t.Errorf("failure does not say the partial copy was removed:\n%s", out)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sitefooter"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sitefooter_test.go" {
		t.Fatalf("target holds %d entries, want only the racing file", len(entries))
	}
	kept, err := os.ReadFile(filepath.Join(dir, "sitefooter", "sitefooter_test.go"))
	if err != nil || string(kept) != string(sentinel) {
		t.Errorf("the racing file was overwritten: %q %v", kept, err)
	}
}

// A copy that fails after writing began leaves nothing behind: the files
// it wrote and the directories it created are removed, back to the state
// before the copy, so the one-shot rule never bricks the target.
func TestGeneratePackageFailureCleansDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestModule(t, dir)
	covT_chdir(t, dir)
	packageWriteFiles = func(files *codegen.FileSet, opts codegen.WriteOptions) error {
		sub := codegen.NewFileSet()
		if err := sub.Add(files.All()[0]); err != nil {
			return err
		}
		if err := codegen.WriteFiles(sub, opts); err != nil {
			return err
		}
		return errors.New("injected failure after the first file")
	}
	t.Cleanup(func() { packageWriteFiles = codegen.WriteFiles })

	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runGeneratePackage([]string{"docpage", "--out=deep/nested/hdr"})
		})
	})
	if code != 1 {
		t.Fatalf("injected write failure must fail the copy, exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Rolled back the partial copy") {
		t.Errorf("failure does not say the partial copy was removed:\n%s", out)
	}
	if !strings.Contains(out, "removed 1 file(s)") {
		t.Errorf("rollback does not report what it removed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep")); !os.IsNotExist(err) {
		t.Errorf("directories the copy created were left behind: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "go.mod" {
		t.Errorf("module dir holds leftovers: %v", entries)
	}
}

// The copy is real code an app can build: copied into a nested dir of a
// temp module, the self-import is rewritten to that module's path,
// nothing references the canonical location, and the package builds,
// vets under -tags chromium (the chromium test pulls chromedp via go
// mod tidy) and passes its own tests. Skipped under -short: it runs the
// go toolchain four times.
func TestGeneratePackageCopyBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go mod tidy, build, vet and test in the copied module")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestModule(t, dir)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	covT_chdir(t, dir)

	out := covT_capStdout(t, func() {
		runGeneratePackage([]string{"siteheader", "--out=web/siteheader"})
	})
	if !strings.Contains(out, "Copied siteheader into web/siteheader") {
		t.Fatalf("copy did not report success:\n%s", out)
	}
	if !strings.Contains(out, "theme.Default().Extend(siteheader.Tokens)") {
		t.Errorf("next steps do not name the theme Extend step:\n%s", out)
	}

	// The import was rewritten to the temp module's path, and no copied
	// file still names the canonical package location.
	test, err := os.ReadFile(filepath.Join(dir, "web", "siteheader", "siteheader_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(test), `"example.com/pkgcopy/web/siteheader"`) {
		t.Errorf("self-import not rewritten:\n%s", test)
	}
	for _, f := range packageCopyFiles(t, filepath.Join(dir, "web", "siteheader")) {
		if strings.Contains(f.content, "cmd/gofastr/packages") {
			t.Errorf("%s still references the canonical location", f.path)
		}
	}
	// The copied package is gofmt-clean whatever the import sort did.
	fmtCmd := exec.Command("gofmt", "-l", "web/siteheader")
	fmtCmd.Dir = dir
	if fmtOut, ferr := fmtCmd.CombinedOutput(); ferr != nil || len(fmtOut) > 0 {
		t.Errorf("copied package is not gofmt-clean:\n%s", fmtOut)
	}

	env := append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"go mod tidy", []string{"mod", "tidy"}},
		{"go build ./...", []string{"build", "./..."}},
		{"go vet -tags chromium ./...", []string{"vet", "-tags", "chromium", "./..."}},
		{"go test ./...", []string{"test", "./..."}},
	} {
		cmd := exec.Command("go", tc.args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			t.Fatalf("%s failed in the copied module (every failure is a defect in the copy):\n%s", tc.name, out)
		}
	}
}

func TestCanonicalPackageGenFilesCurrent(t *testing.T) {
	infos, err := canonicalPackageInfos()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) < 3 {
		t.Fatalf("canonical packages = %v, want siteheader, sitefooter and docpage", infos)
	}
	builtins := style.ThemeToTokens(style.DefaultTheme())
	for _, p := range infos {
		files, err := readEmbeddedPackage(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		byName := map[string]string{}
		for _, f := range files {
			byName[f.Name] = f.Content
		}
		// The copy path's own gate: a dirty sheet fails every copy and
		// the blueprint, so it fails here first.
		if err := checkCanonicalPackageSheets(p.Name, byName); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}

		if tokensSrc, ok := byName[p.Name+".tokens.css"]; ok {
			src := ownstyle.SheetSource{File: p.Name + "/" + p.Name + ".tokens.css", Src: tokensSrc}
			parsed, _, _ := ownstyle.CheckTokenFiles([]ownstyle.SheetSource{src}, builtins)
			want, err := ownstyle.GenerateTokensFile(p.Name, src.Src, parsed[src.File], p.Name, false)
			if err != nil {
				t.Fatalf("%s: %v", p.Name, err)
			}
			if got := byName[ownstyle.GeneratedTokensFileName(p.Name)]; got != want {
				t.Errorf("%s is not what gen styles produces from %s.tokens.css (edit the sheet, run `gofastr gen styles`, commit both)", ownstyle.GeneratedTokensFileName(p.Name), p.Name)
			}
		}

		css, ok := byName[p.Name+".style.css"]
		if !ok {
			t.Errorf("%s has no .style.css", p.Name)
			continue
		}
		var model *ownstyle.SheetModel
		sheet, _ := ownstyle.Parse(css)
		if sheet != nil {
			model, _ = ownstyle.Model(sheet)
		}
		want, err := ownstyle.GenerateFile(p.Name, ownstyle.KindScoped, css, model, p.Name, false)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		if got := byName[ownstyle.GeneratedFileName(p.Name)]; got != want {
			t.Errorf("%s is not what gen styles produces from %s.style.css (edit the sheet, run `gofastr gen styles`, commit both)", ownstyle.GeneratedFileName(p.Name), p.Name)
		}
	}
}

// writeTestModule writes a minimal go.mod resolving the framework
// through a replace to the repo root, the shape every generated-module
// gate in this package uses.
func writeTestModule(t *testing.T, dir string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	version, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/pkgcopy\n\ngo "+version+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
}

type copiedFile struct {
	path, content string
}

// packageCopyFiles reads every file under dir for the no-leftovers
// assertion.
func packageCopyFiles(t *testing.T, dir string) []copiedFile {
	t.Helper()
	var out []copiedFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return werr
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out = append(out, copiedFile{path: path, content: string(body)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
