package unmountcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"golang.org/x/mod/module"
)

func printUnmountHelp() {
	fmt.Println(`gofastr unmount: copy a gofastr package into your app

Usage:
  gofastr unmount                     List the unmounted packages
  gofastr unmount <package> [--out=<dir>] [--dry-run]

<package> is an import path under github.com/DonaldMurillo/gofastr, or
the part after it (battery/auth). Unmount copies the package and the
packages under it from the gofastr version your go.mod requires into
<dir> (default unmounted/<package> at the module root), rewires your
module's imports to the copy, and records it in the unmounted section of
gofastr.settings.yml. Tests and testdata are not copied, unless the
package embeds from testdata.

The copy is yours: edit it freely. Fixes to the framework's package no
longer reach your app; gofastr verify and gofastr upgrade say when the
framework has moved past the copied version.

Unmount refuses a package that imports a gofastr internal package, or
that a package outside your module imports (another gofastr package, a
plugin): the binary would carry two versions of its types. gofastr remount <package> goes
back to the framework's package.`)
}

func printRemountHelp() {
	fmt.Println(`gofastr remount: go back to the framework's package

Usage:
  gofastr remount <package> [--force] [--dry-run]

<package> is an unmounted package's import path, the part after
github.com/DonaldMurillo/gofastr, or the copy's directory. Remount
rewires your module's imports back to the framework's package, deletes
the copy and removes its entry from gofastr.settings.yml.

Remount refuses a copy that changed since it was unmounted (an edited,
added or deleted file) and names the files. --force deletes the copy
anyway, and the changes with it.`)
}

// parseUnmountArgs parses the flags unmount and remount share: one
// optional positional, --dry-run, plus --out for unmount and --force for
// remount.
func parseUnmountArgs(cmd string, args []string) (pkg, outDir string, dryRun, force, ok bool) {
	usage := "Usage: gofastr unmount [<package>] [--out=DIR] [--dry-run]"
	if cmd == "remount" {
		usage = "Usage: gofastr remount <package> [--force] [--dry-run]"
	}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dry-run":
			dryRun = true
		case arg == "--force" && cmd == "remount":
			force = true
		case strings.HasPrefix(arg, "--out=") && cmd == "unmount":
			outDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--out" && cmd == "unmount":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				cli.Fail("--out requires a directory argument")
				cli.Info("%s", usage)
				return "", "", false, false, false
			}
			i++
			outDir = args[i]
		case strings.HasPrefix(arg, "-"):
			cli.Fail("Unknown flag %q for `gofastr %s`", arg, cmd)
			cli.Info("%s", usage)
			return "", "", false, false, false
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) > 1 {
		cli.Fail("`gofastr %s` takes one package", cmd)
		cli.Info("%s", usage)
		return "", "", false, false, false
	}
	if len(positionals) == 1 {
		pkg = positionals[0]
	}
	if pkg == "" && (cmd == "remount" || outDir != "" || dryRun) {
		cli.Fail("`gofastr %s` needs a package", cmd)
		cli.Info("%s", usage)
		return "", "", false, false, false
	}
	return pkg, outDir, dryRun, force, true
}

// unmountSourcePath turns the argument into the upstream import path:
// a full path under the gofastr module, or the part after it.
func unmountSourcePath(arg string) (string, error) {
	p := strings.TrimSuffix(strings.TrimSpace(arg), "/")
	if !strings.HasPrefix(p, project.GofastrModule+"/") {
		if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") {
			return "", fmt.Errorf("%s is not a gofastr package: unmount copies packages under %s", arg, project.GofastrModule)
		}
		p = project.GofastrModule + "/" + p
	}
	if err := module.CheckImportPath(p); err != nil || p != path.Clean(p) {
		return "", fmt.Errorf("%q is not a package import path", arg)
	}
	for _, elem := range strings.Split(strings.TrimPrefix(p, project.GofastrModule+"/"), "/") {
		if elem == "internal" {
			return "", fmt.Errorf("%s is internal to gofastr: only gofastr imports it, so there is nothing of yours to rewire", p)
		}
	}
	if p == appgen.UnmountRegistryImport {
		return "", fmt.Errorf("%s holds the registry every copy reports to, so it cannot be unmounted", p)
	}
	return p, nil
}

func isGofastrPackage(p string) bool { return appgen.InTree(p, project.GofastrModule) }

// inModule reports whether go list's package p belongs to the module at
// modulePath, counting the external test package (p_test) and the test
// main (p.test) that `go list -test` adds for a package of the module.
func inModule(p, modulePath string) bool {
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".test"), "_test")
	return appgen.InTree(p, modulePath)
}

// stripTestVariant drops the " [pkg.test]" suffix go list puts on a
// package recompiled for a test.
func stripTestVariant(p string) string {
	if i := strings.Index(p, " ["); i >= 0 {
		return p[:i]
	}
	return p
}

// goListModule runs `go list -m -json <gofastr>` in dir. A var so tests
// stand in a module.
var goListModule = func(ctx context.Context, dir string) ([]byte, error) {
	return runGoList(ctx, dir, "list", "-m", "-json", project.GofastrModule)
}

// goListDeps lists every package pattern's build needs, each with its
// imports. With tests it is the app's own build, tests included, and an
// error is an error. Without, it is an upstream tree the app may not
// build yet (-e): a subpackage whose module go.sum lacks still lists what
// it imports from gofastr. A var so tests stand in a build.
var goListDeps = func(ctx context.Context, dir, pattern string, tests bool) ([]byte, error) {
	mode := "-e"
	if tests {
		mode = "-test"
	}
	return runGoList(ctx, dir, "list", "-deps", mode, "-f", `{{.ImportPath}}{{range .Imports}}{{"\t"}}{{.}}{{end}}`, pattern)
}

func runGoList(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %v\n%s", strings.Join(args[:2], " "), err, cli.ScrubTerminalOutput(strings.TrimSpace(stderr.String())))
	}
	return out, nil
}

// gofastrModuleSource finds the gofastr module the app builds against:
// its directory and the version a copy records. A replace to a local
// checkout, or to any other module, records dev: it is no release anyone
// can compare against.
func gofastrModuleSource(ctx context.Context, moduleRoot string) (dir, version string, err error) {
	out, err := goListModule(ctx, moduleRoot)
	if err != nil {
		return "", "", err
	}
	var m struct {
		Version string
		Dir     string
		Main    bool
		Replace *struct{ Dir string }
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return "", "", fmt.Errorf("read go list -m: %w", err)
	}
	if m.Main {
		return "", "", errors.New("this is gofastr's own module: unmount copies gofastr packages into an app that requires it")
	}
	dir, version = m.Dir, appgen.MarkerVersion(m.Version)
	if m.Replace != nil {
		dir, version = m.Replace.Dir, "dev"
	}
	if dir == "" {
		return "", "", fmt.Errorf("%s is not in the module cache: run `go mod download %s` first", project.GofastrModule, project.GofastrModule)
	}
	return dir, version, nil
}

// readUnmountTree reads the package at source and every package under
// it from the module directory, through an os.Root so nothing outside
// the tree is read. Tests, testdata, vendor, hidden and _-prefixed
// directories and nested modules are skipped: they are not part of what
// the app builds. The root must be a Go package.
func readUnmountTree(moduleDir, source, version string) (appgen.UnmountTree, error) {
	rel := strings.TrimPrefix(source, project.GofastrModule+"/")
	root, err := os.OpenRoot(filepath.Join(moduleDir, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return appgen.UnmountTree{}, fmt.Errorf("gofastr %s has no package %s", version, source)
		}
		return appgen.UnmountTree{}, err
	}
	defer func() { _ = root.Close() }()
	tree := appgen.UnmountTree{Source: source, Version: version}
	rootIsPackage := false
	embeds := map[string][]string{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p == "." {
				return nil
			}
			if (name == "vendor" || name == "testdata" || name == "node_modules" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) && !embeddedDir(root, path.Dir(p), name, embeds) {
				return fs.SkipDir
			}
			if _, err := root.Stat(path.Join(p, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if name == appgen.UnmountHookFile {
			return fmt.Errorf("%s/%s already exists upstream; unmount writes its own file by that name", source, p)
		}
		body, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		rootIsPackage = rootIsPackage || (path.Dir(p) == "." && strings.HasSuffix(name, ".go"))
		tree.Files = append(tree.Files, appgen.PackageFile{Name: p, Content: string(body)})
		return nil
	})
	if err != nil {
		return appgen.UnmountTree{}, err
	}
	if !rootIsPackage {
		return appgen.UnmountTree{}, fmt.Errorf("%s holds no Go files of its own: unmount the package you import", source)
	}
	slices.SortFunc(tree.Files, func(a, b appgen.PackageFile) int { return strings.Compare(a.Name, b.Name) })
	return tree, nil
}

// embeddedDir reports whether a non-test Go file in dir embeds from its
// subdirectory name (`//go:embed testdata/axe.min.js`, `all:_assets`).
// The go tool skips such a directory as a package, but the package needs
// its files to build. embeds caches each directory's embed patterns.
func embeddedDir(root *os.Root, dir, name string, embeds map[string][]string) bool {
	patterns, ok := embeds[dir]
	if !ok {
		patterns = []string{}
		entries, _ := fs.ReadDir(root.FS(), dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			body, err := root.ReadFile(path.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for line := range strings.Lines(string(body)) {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "//go:embed "); ok {
					patterns = append(patterns, strings.Fields(rest)...)
				}
			}
		}
		embeds[dir] = patterns
	}
	for _, pat := range patterns {
		pat = strings.TrimPrefix(strings.Trim(pat, "\"`"), "all:")
		if first, _, _ := strings.Cut(pat, "/"); first == name {
			return true
		}
	}
	return false
}

// checkUnmountInternal refuses a tree that imports a gofastr internal
// package from outside itself: Go lets only gofastr import those, so the
// copy would not build.
func checkUnmountInternal(tree appgen.UnmountTree) error {
	var found []string
	for _, f := range tree.Files {
		if !strings.HasSuffix(f.Name, ".go") {
			continue
		}
		pf, err := parser.ParseFile(token.NewFileSet(), f.Name, f.Content, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", tree.Source, f.Name, err)
		}
		for _, spec := range pf.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !isGofastrPackage(imp) || appgen.InTree(imp, tree.Source) {
				continue
			}
			if slices.Contains(strings.Split(imp, "/"), "internal") {
				found = append(found, fmt.Sprintf("%s imports %s", f.Name, imp))
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("%s imports gofastr internal packages, which only gofastr may import, so a copy would not build:\n  %s", tree.Source, strings.Join(found, "\n  "))
}

// unmountImporters returns the packages outside the app's module and
// outside the tree that import a package in the tree, each with the tree
// packages it imports: other gofastr packages, and third-party ones such
// as plugins. It reads the app's build (tests included) and the tree's
// own build, since a subpackage the app does not import yet can still
// depend on a package that imports the tree's root. The rewire reaches
// only the app's own files, so a copy alongside such an importer would put
// two versions of the tree's types in one binary.
func unmountImporters(ctx context.Context, moduleRoot, modulePath, source string) (map[string][]string, error) {
	app, err := goListDeps(ctx, moduleRoot, "./...", true)
	if err != nil {
		return nil, fmt.Errorf("list the app's build: %w", err)
	}
	tree, err := goListDeps(ctx, moduleRoot, source+"/...", false)
	if err != nil {
		return nil, fmt.Errorf("list %s's build: %w", source, err)
	}
	importers := map[string][]string{}
	importersIn(app, modulePath, source, importers)
	importersIn(tree, modulePath, source, importers)
	return importers, nil
}

// importersIn adds to importers each package of a goListDeps listing that
// is outside the module and outside the tree at source and imports a
// package in the tree, with the tree packages it imports, sorted.
func importersIn(listing []byte, modulePath, source string, importers map[string][]string) {
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Split(line, "\t")
		pkg := stripTestVariant(fields[0])
		if inModule(pkg, modulePath) || appgen.InTree(pkg, source) {
			continue
		}
		for _, imp := range fields[1:] {
			imp = stripTestVariant(imp)
			if appgen.InTree(imp, source) && !slices.Contains(importers[pkg], imp) {
				importers[pkg] = append(importers[pkg], imp)
				slices.Sort(importers[pkg])
			}
		}
	}
}

// readUnmountFiles reads the file sums back from a root hook file.
func readUnmountFiles(hook string) map[string]string {
	files := map[string]string{}
	in := false
	for line := range strings.Lines(hook) {
		line = strings.TrimRight(line, "\r\n")
		if line == appgen.UnmountFilesHeader {
			in = true
			continue
		}
		if !in || line == "//" {
			continue
		}
		rest, ok := strings.CutPrefix(line, "//\t")
		if !ok {
			break
		}
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			break
		}
		name, _ := strconv.Unquote(quoted)
		files[name] = strings.TrimSpace(rest[len(quoted):])
	}
	return files
}

// importRewire is one Go file whose imports a rewire changes, with its
// bytes before and after.
type importRewire struct {
	name     string
	mode     fs.FileMode
	old, new string
}

// planImportRewire finds every Go file in the module whose imports of from
// (or a package under it) move to the same package under to. The copy's
// own directory, vendor, testdata, hidden and _-prefixed directories and
// nested modules are skipped. A file that was gofmt-clean is formatted
// again after the rewrite, since import order follows the path; any other
// file keeps its formatting.
func planImportRewire(moduleRoot, skip, from, to string) ([]importRewire, error) {
	root, err := os.OpenRoot(moduleRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var plan []importRewire
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p == "." {
				return nil
			}
			if p == skip || name == "vendor" || name == "testdata" || name == "node_modules" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
			if _, err := root.Stat(path.Join(p, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(name, ".go") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		body, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		old := string(body)
		rewritten, changed, err := appgen.RewriteImportPrefix(old, from, to)
		if err != nil || !changed {
			return nil
		}
		if formatted, ferr := format.Source(body); ferr == nil && string(formatted) == old {
			rewritten = appgen.FormatGenerated(name, rewritten)
		}
		plan = append(plan, importRewire{name: p, mode: info.Mode().Perm(), old: old, new: rewritten})
		return nil
	})
	return plan, err
}

// applyImportRewire writes a rewire plan (new over old, or old over new
// to undo it) through an os.Root at the module root. It stops at the
// first failure and returns how many files it touched, the failed one
// included: a write that fails after the truncate has already changed
// that file, so the undo must cover it too.
func applyImportRewire(moduleRoot string, plan []importRewire, undo bool) (int, error) {
	root, err := os.OpenRoot(moduleRoot)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	for i, r := range plan {
		content := r.new
		if undo {
			content = r.old
		}
		if err := root.WriteFile(r.name, []byte(content), r.mode); err != nil {
			return i + 1, fmt.Errorf("%s: %w", r.name, err)
		}
	}
	return len(plan), nil
}

// checkUnmountRegistry refuses a gofastr version that predates
// core/unmounted: every copy's unmounted.gen.go imports it from the
// gofastr go.mod requires, so the copy would not build.
func checkUnmountRegistry(moduleDir, version string) error {
	rel := strings.TrimPrefix(appgen.UnmountRegistryImport, project.GofastrModule+"/")
	if _, err := os.Stat(filepath.Join(moduleDir, filepath.FromSlash(rel), "unmounted.go")); err != nil {
		return fmt.Errorf("gofastr %s has no %s, which every copy registers with, so the copy would not build: upgrade gofastr first", version, appgen.UnmountRegistryImport)
	}
	return nil
}

// RunUnmount is `gofastr unmount [<package>]`: with no package it lists
// the copies the settings file records; with one it copies that package's
// tree into the app, rewires the module's imports to the copy and records
// it.
func RunUnmount(args []string) {
	if cli.HasHelpFlag(args) {
		printUnmountHelp()
		return
	}
	arg, outDir, dryRun, _, ok := parseUnmountArgs("unmount", args)
	if !ok {
		cli.OsExit(1)
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(wd)
	if modulePath == "" {
		cli.Fail("%v", appgen.ErrPackageNoModule)
		cli.OsExit(1)
		return
	}
	settingsPath, entries, err := appgen.UnmountedEntries(moduleRoot)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if arg == "" {
		listUnmounted(entries)
		return
	}
	if err := unmountPackage(arg, outDir, dryRun, wd, modulePath, moduleRoot, settingsPath, entries); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
}

func listUnmounted(entries []settings.Unmounted) {
	if len(entries) == 0 {
		fmt.Println("No unmounted packages. Copy one: gofastr unmount <package>")
		return
	}
	fmt.Println("Unmounted packages:")
	for _, e := range entries {
		fmt.Printf("  %s %s -> %s (%s)\n", e.Source, e.Version, e.Path, e.Date)
	}
}

func unmountPackage(arg, outDir string, dryRun bool, wd, modulePath, moduleRoot, settingsPath string, entries []settings.Unmounted) error {
	source, err := unmountSourcePath(arg)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(source, project.GofastrModule+"/")
	if outDir == "" {
		outDir = filepath.Join(moduleRoot, "unmounted", filepath.FromSlash(rel))
	}
	tgt, err := appgen.ResolvePackageTarget(path.Base(source), outDir, wd)
	if err != nil {
		return err
	}
	if tgt.AnchorDir != moduleRoot {
		return fmt.Errorf("target %s is in the module at %s, not in this module: unmount rewires this module's imports, so the copy must live in it", tgt.Display, tgt.AnchorDir)
	}
	tgt.Display = filepath.ToSlash(tgt.Root)
	entry := settings.Unmounted{Source: source, Path: tgt.Display, Date: appgen.UnmountNow().Format(time.DateOnly)}
	if err := appgen.CheckUnmountOverlap(entry, entries, true); err != nil {
		return err
	}
	if dirEntries, derr := os.ReadDir(tgt.Dir); derr == nil && len(dirEntries) > 0 {
		return fmt.Errorf("%s already exists and is not empty: move it away or pick another --out", tgt.Display)
	}

	ctx := context.Background()
	moduleDir, version, err := gofastrModuleSource(ctx, moduleRoot)
	if err != nil {
		return err
	}
	entry.Version = version
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("cannot record the copy: %w", err)
	}
	if err := checkUnmountRegistry(moduleDir, version); err != nil {
		return err
	}
	tree, err := readUnmountTree(moduleDir, source, version)
	if err != nil {
		return err
	}
	if err := checkUnmountInternal(tree); err != nil {
		return err
	}
	importers, err := unmountImporters(ctx, moduleRoot, modulePath, source)
	if err != nil {
		return err
	}
	if len(importers) > 0 {
		var lines []string
		for _, pkg := range slices.Sorted(maps.Keys(importers)) {
			lines = append(lines, pkg+" imports "+strings.Join(importers[pkg], ", "))
		}
		return fmt.Errorf("packages outside this module that the app's build compiles import %s; the rewire cannot reach them, so a copy would put two versions of its types in one binary:\n  %s", source, strings.Join(lines, "\n  "))
	}
	files, err := appgen.BuildUnmountCopy(tree, tgt.ImportPath)
	if err != nil {
		return err
	}
	plan, err := planImportRewire(moduleRoot, filepath.ToSlash(tgt.Root), source, tgt.ImportPath)
	if err != nil {
		return fmt.Errorf("find the imports to rewire: %w", err)
	}
	if dryRun {
		fmt.Printf("Would copy %s %s into %s:\n", source, version, tgt.Display)
		for _, f := range files {
			fmt.Printf("  %s\n", path.Join(tgt.Display, f.Name))
		}
		fmt.Printf("Would rewire the imports in %d file(s):\n", len(plan))
		for _, r := range plan {
			fmt.Printf("  %s\n", r.name)
		}
		fmt.Printf("Would record it in %s\n", settingsPath)
		return nil
	}
	undoCopy, err := appgen.WritePackageCopy(tgt, files)
	if err != nil {
		return err
	}
	if n, err := applyImportRewire(moduleRoot, plan, false); err != nil {
		_, _ = applyImportRewire(moduleRoot, plan[:n], true)
		undoCopy()
		return fmt.Errorf("rewire imports: %w; the copy and the files already rewired were put back", err)
	}
	if err := settings.SetUnmounted(settingsPath, append(slices.Clone(entries), entry)); err != nil {
		_, _ = applyImportRewire(moduleRoot, plan, true)
		undoCopy()
		return fmt.Errorf("record the copy: %w; the copy and the rewired imports were put back", err)
	}
	cli.Success("Unmounted %s %s into %s (%d file(s)); %d file(s) now import the copy", source, version, tgt.Display, len(files), len(plan))
	if len(plan) == 0 {
		cli.Info("Nothing in this module imports %s yet; import %s to use the copy.", source, tgt.ImportPath)
	}
	cli.Info("The copy is yours: fixes to %s no longer reach this app. `gofastr remount %s` goes back.", source, rel)
	return nil
}

// RunRemount is `gofastr remount <package>`: it rewires the module's
// imports from a copy back to the framework's package, removes the copy's
// settings entry and deletes the copy.
func RunRemount(args []string) {
	if cli.HasHelpFlag(args) {
		printRemountHelp()
		return
	}
	arg, _, dryRun, force, ok := parseUnmountArgs("remount", args)
	if !ok {
		cli.OsExit(1)
		return
	}
	if err := remountPackage(arg, dryRun, force); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
}

func remountPackage(arg string, dryRun, force bool) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(wd)
	if modulePath == "" {
		return appgen.ErrPackageNoModule
	}
	settingsPath, entries, err := appgen.UnmountedEntries(moduleRoot)
	if err != nil {
		return err
	}
	idx, err := findUnmounted(arg, wd, moduleRoot, entries)
	if err != nil {
		return err
	}
	if idx < 0 {
		return fmt.Errorf("%s is not unmounted here: %s lists no copy of it (`gofastr unmount` lists the copies)", arg, settingsPath)
	}
	entry := entries[idx]
	copyImport := modulePath + "/" + entry.Path

	ctx := context.Background()
	moduleDir, version, err := gofastrModuleSource(ctx, moduleRoot)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(entry.Source, project.GofastrModule+"/")
	if _, err := os.Stat(filepath.Join(moduleDir, filepath.FromSlash(rel))); err != nil {
		return fmt.Errorf("gofastr %s has no package %s, so the rewired imports would not build: move to a gofastr version that has it first", version, entry.Source)
	}
	if err := checkRemountImporters(ctx, moduleRoot, modulePath, entry, entries); err != nil {
		return err
	}

	// A copy already deleted holds no changes to lose: remount still
	// rewires the imports back and drops the entry.
	var changed []string
	if _, err := os.Stat(filepath.Join(moduleRoot, filepath.FromSlash(entry.Path))); errors.Is(err, fs.ErrNotExist) {
		cli.Info("The copy at %s is gone already; rewiring the imports back to %s and dropping its entry", entry.Path, entry.Source)
	} else if changed, err = unmountCopyChanges(moduleRoot, entry); err != nil {
		return err
	}
	if len(changed) > 0 && !force && !dryRun {
		return fmt.Errorf("the copy at %s changed since it was unmounted, and remount would delete the changes:\n  %s\nKeep them by carrying them upstream or in your own package; --force deletes them", entry.Path, strings.Join(changed, "\n  "))
	}
	plan, err := planImportRewire(moduleRoot, entry.Path, copyImport, entry.Source)
	if err != nil {
		return fmt.Errorf("find the imports to rewire: %w", err)
	}
	if dryRun {
		fmt.Printf("Would rewire the imports in %d file(s) back to %s:\n", len(plan), entry.Source)
		for _, r := range plan {
			fmt.Printf("  %s\n", r.name)
		}
		fmt.Printf("Would delete %s and its entry in %s\n", entry.Path, settingsPath)
		if len(changed) > 0 && !force {
			fmt.Printf("Changed since unmount, so remount needs --force and deletes them:\n")
			for _, c := range changed {
				fmt.Printf("  %s\n", c)
			}
		}
		return nil
	}
	if len(changed) > 0 {
		cli.Warn("--force is deleting %d changed file(s) in %s:", len(changed), entry.Path)
		for _, c := range changed {
			cli.Warn("    %s", c)
		}
	}
	if n, err := applyImportRewire(moduleRoot, plan, false); err != nil {
		_, _ = applyImportRewire(moduleRoot, plan[:n], true)
		return fmt.Errorf("rewire imports: %w; the files already rewired were put back", err)
	}
	if err := settings.SetUnmounted(settingsPath, slices.Delete(slices.Clone(entries), idx, idx+1)); err != nil {
		_, _ = applyImportRewire(moduleRoot, plan, true)
		return fmt.Errorf("remove the settings entry: %w; the rewired imports were put back", err)
	}
	root, err := os.OpenRoot(moduleRoot)
	if err == nil {
		err = root.RemoveAll(filepath.FromSlash(entry.Path))
		// Parents the copy left empty (unmounted/battery/) go too; Remove
		// refuses one that still holds anything.
		for d := path.Dir(entry.Path); err == nil && d != "."; d = path.Dir(d) {
			if root.Remove(filepath.FromSlash(d)) != nil {
				break
			}
		}
		_ = root.Close()
	}
	if err != nil {
		return fmt.Errorf("the imports and settings point upstream again, but deleting %s failed: %w", entry.Path, err)
	}
	cli.Success("Remounted %s: %d file(s) import it again, and %s is gone", entry.Source, len(plan), entry.Path)
	return nil
}

// checkRemountImporters refuses a remount that brings back framework
// packages importing another package this app has unmounted: those would
// import the framework's version while the app uses its copy, the same
// two versions of one type unmount refuses.
func checkRemountImporters(ctx context.Context, moduleRoot, modulePath string, entry settings.Unmounted, entries []settings.Unmounted) error {
	if len(entries) < 2 {
		return nil
	}
	listing, err := goListDeps(ctx, moduleRoot, entry.Source+"/...", false)
	if err != nil {
		return fmt.Errorf("list %s's build: %w", entry.Source, err)
	}
	var lines []string
	for _, e := range entries {
		if e.Path == entry.Path {
			continue
		}
		importers := map[string][]string{}
		importersIn(listing, modulePath, e.Source, importers)
		for _, pkg := range slices.Sorted(maps.Keys(importers)) {
			lines = append(lines, fmt.Sprintf("%s imports %s, unmounted at %s", pkg, strings.Join(importers[pkg], ", "), e.Path))
		}
	}
	if len(lines) > 0 {
		return fmt.Errorf("remounting %s brings back framework packages that import packages this app has unmounted, so the build would hold two versions of their types:\n  %s\nRemount those first", entry.Source, strings.Join(lines, "\n  "))
	}
	return nil
}

// findUnmounted finds the entry an argument names: its path from the
// module root or the working directory, or its source (full or after the
// gofastr module path). A source with several copies is ambiguous, and
// the error lists their paths.
func findUnmounted(arg, wd, moduleRoot string, entries []settings.Unmounted) (int, error) {
	source, _ := unmountSourcePath(arg)
	local := ""
	abs := arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(wd, abs)
	}
	if rel, err := filepath.Rel(moduleRoot, abs); err == nil {
		local = filepath.ToSlash(rel)
	}
	for i, e := range entries {
		if e.Path == path.Clean(filepath.ToSlash(arg)) || e.Path == local {
			return i, nil
		}
	}
	found, paths := -1, []string(nil)
	for i, e := range entries {
		if e.Source == arg || e.Source == source {
			found = i
			paths = append(paths, e.Path)
		}
	}
	if len(paths) > 1 {
		return -1, fmt.Errorf("%s has %d copies (%s): name the one to remount by its directory", arg, len(paths), strings.Join(paths, ", "))
	}
	return found, nil
}

// unmountCopyChanges lists what changed in a copy since unmount wrote it,
// against the sums the root hook recorded: an edited file, one the list
// does not name (added), and one it names that is gone (deleted). The
// root hook answers for itself through its marker; with it gone or
// edited, the list cannot be trusted and every file counts as changed.
func unmountCopyChanges(moduleRoot string, entry settings.Unmounted) ([]string, error) {
	root, err := os.OpenRoot(filepath.Join(moduleRoot, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, fmt.Errorf("open the copy at %s: %w", entry.Path, err)
	}
	defer func() { _ = root.Close() }()
	var changed []string
	listed := map[string]string{}
	hook, err := root.ReadFile(appgen.UnmountHookFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		changed = append(changed, appgen.UnmountHookFile+" (deleted)")
	case err != nil:
		return nil, err
	default:
		if edited, ok := codegen.Edited(appgen.UnmountHookFile, string(hook)); !ok || edited {
			changed = append(changed, appgen.UnmountHookFile+" (edited)")
		} else {
			listed = readUnmountFiles(string(hook))
		}
	}
	seen := map[string]bool{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || p == appgen.UnmountHookFile {
			return nil
		}
		seen[p] = true
		if !d.Type().IsRegular() {
			changed = append(changed, p+" (not a regular file)")
			return nil
		}
		body, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		sum, ok := listed[p]
		switch {
		case !ok:
			changed = append(changed, p+" (added)")
		case codegen.Sum(string(body)) != sum:
			changed = append(changed, p+" (edited)")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !seen[name] {
			changed = append(changed, name+" (deleted)")
		}
	}
	slices.Sort(changed)
	return changed, nil
}
