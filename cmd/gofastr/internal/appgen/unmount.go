package appgen

import (
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"golang.org/x/mod/module"
)

// UnmountHookFile is the generated file in each package of a copy whose
// init() registers the copy with core/unmounted.
const UnmountHookFile = "unmounted.gen.go"

// UnmountRegistryImport is the package the hook registers with. A copy of
// it would register with itself, so it can never be unmounted.
const UnmountRegistryImport = project.GofastrModule + "/core/unmounted"

// UnmountFilesHeader opens the root hook's list of the copy's other files with their sums.
const UnmountFilesHeader = "// The files copied, and their sums when copied:"

// UnmountNow is the clock the settings entry's date reads; a var so tests
// pin it.
var UnmountNow = time.Now

// UnmountTree is a package tree to copy: its upstream root import path, its version and its files.
type UnmountTree struct {
	Source  string
	Version string
	Files   []PackageFile
}

func InTree(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// RewriteImportPrefix rewrites every import of from, or of a package under it, to the same package under to.
func RewriteImportPrefix(src, from, to string) (string, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return "", false, fmt.Errorf("parse imports: %w", err)
	}
	var out []byte
	changed := false
	off := 0
	for _, spec := range f.Imports {
		p, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil || !InTree(p, from) {
			continue
		}
		start := fset.Position(spec.Path.Pos()).Offset
		end := fset.Position(spec.Path.End()).Offset
		out = append(out, src[off:start]...)
		out = append(out, strconv.Quote(to+strings.TrimPrefix(p, from))...)
		off = end
		changed = true
	}
	out = append(out, src[off:]...)
	return string(out), changed, nil
}

// BuildUnmountCopy renders the copy of tree for targetImport, with its imports rewritten, its markers and a hook file per package.
func BuildUnmountCopy(tree UnmountTree, targetImport string) ([]GeneratedFile, error) {
	if err := module.CheckImportPath(targetImport); err != nil {
		return nil, fmt.Errorf("cannot copy %s: %w", tree.Source, err)
	}
	marker := codegen.Marker{Generator: project.UnmountGenerator, Kind: codegen.KindScaffold, Version: tree.Version, Source: tree.Source}
	var files []GeneratedFile
	packages, ignored := map[string]string{}, map[string]string{}
	for _, f := range tree.Files {
		content := f.Content
		if !strings.HasSuffix(f.Name, ".go") {
			files = append(files, GeneratedFile{Name: f.Name, Content: content})
			continue
		}
		rewritten, _, err := RewriteImportPrefix(content, tree.Source, targetImport)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", tree.Source, f.Name, err)
		}
		content = FormatGenerated(f.Name, rewritten)
		if _, _, ok := codegen.ReadMarker(f.Name, content); !ok {
			if content, err = codegen.Stamp(f.Name, content, marker); err != nil {
				return nil, err
			}
		}
		files = append(files, GeneratedFile{Name: f.Name, Content: content})
		if strings.HasSuffix(f.Name, "_test.go") {
			continue
		}
		name, err := packageName(f.Content)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", tree.Source, f.Name, err)
		}
		dir := path.Dir(f.Name)
		if buildIgnored(f.Content) {
			ignored[dir] = name
		} else if packages[dir] == "" {
			packages[dir] = name
		}
	}
	for dir, name := range ignored {
		if packages[dir] == "" {
			packages[dir] = name
		}
	}
	hook := func(dir string, listed map[string]string) (GeneratedFile, error) {
		content, err := unmountHook(packages[dir], tree, targetImport, listed)
		if err == nil {
			content, err = codegen.Stamp(UnmountHookFile, content, marker)
		}
		return GeneratedFile{Name: path.Join(dir, UnmountHookFile), Content: content}, err
	}
	for _, dir := range slices.Sorted(maps.Keys(packages)) {
		if dir == "." {
			continue
		}
		h, err := hook(dir, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, h)
	}
	listed := make(map[string]string, len(files))
	for _, f := range files {
		listed[f.Name] = codegen.Sum(f.Content)
	}
	h, err := hook(".", listed)
	if err != nil {
		return nil, err
	}
	files = append(files, h)
	slices.SortFunc(files, func(a, b GeneratedFile) int { return strings.Compare(a.Name, b.Name) })
	return files, nil
}

func packageName(src string) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}

func buildIgnored(src string) bool {
	for line := range strings.Lines(src) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return false
		}
		return !expr.Eval(func(tag string) bool { return tag != "ignore" })
	}
	return false
}

func unmountHook(pkg string, tree UnmountTree, targetImport string, files map[string]string) (string, error) {
	if !token.IsIdentifier(pkg) {
		return "", fmt.Errorf("%s: package name %q is not an identifier", tree.Source, pkg)
	}
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	b.WriteString("import gofastrunmounted " + strconv.Quote(UnmountRegistryImport) + "\n\n")
	b.WriteString("// This package is an unmounted copy of " + tree.Source + " at " + tree.Version + ".\n")
	b.WriteString("// `gofastr remount " + tree.Source + "` goes back to the framework's package.\n")
	if len(files) > 0 {
		b.WriteString("//\n" + UnmountFilesHeader + "\n//\n")
		for _, name := range slices.Sorted(maps.Keys(files)) {
			b.WriteString("//\t" + strconv.Quote(name) + " " + files[name] + "\n")
		}
	}
	b.WriteString("\nfunc init() {\n")
	fmt.Fprintf(&b, "\tgofastrunmounted.Register(gofastrunmounted.Copy{Source: %s, Version: %s, Path: %s})\n",
		strconv.Quote(tree.Source), strconv.Quote(tree.Version), strconv.Quote(targetImport))
	b.WriteString("}\n")
	return FormatGenerated(UnmountHookFile, b.String()), nil
}

// WritePackageCopy writes a copy into its resolved target, rolling back on failure; on success it returns the same rollback as undo.
func WritePackageCopy(tgt packageTarget, files []GeneratedFile) (undo func(), err error) {
	fileSet := codegen.NewFileSet()
	for _, f := range files {
		if err := fileSet.Add(codegen.GeneratedFile{Path: f.Name, Content: f.Content}); err != nil {
			return nil, err
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(tgt.AnchorDir); err != nil {
		return nil, err
	}
	existed := packageDirChainExisted(tgt)
	writeErr := packageWriteFiles(fileSet, codegen.WriteOptions{
		OutputRoot:   tgt.Root,
		SkipManifest: true,
		Conflict:     codegen.ConflictRefuse,
	})
	_ = os.Chdir(wd)
	if writeErr != nil {
		filesRemoved, dirsRemoved := rollbackPackageCopy(tgt, files, existed)
		return nil, fmt.Errorf("write the copy: %w\nRolled back the partial copy: removed %d file(s) and %d dir(s) this run created in %s; anything this run did not write was left untouched", writeErr, filesRemoved, dirsRemoved, tgt.Display)
	}
	return func() { rollbackPackageCopy(tgt, files, existed) }, nil
}

// CheckUnmountOverlap refuses a copy whose path, or for an unmount whose source, overlaps another copy's.
func CheckUnmountOverlap(entry settings.Unmounted, entries []settings.Unmounted, rewire bool) error {
	for _, e := range entries {
		switch {
		case InTree(entry.Path, e.Path) || InTree(e.Path, entry.Path):
			return fmt.Errorf("%s overlaps %s, the copy of %s", entry.Path, e.Path, e.Source)
		case !rewire:
		case e.Source == entry.Source:
			return fmt.Errorf("%s is already unmounted at %s", e.Source, e.Path)
		case InTree(entry.Source, e.Source):
			return fmt.Errorf("%s is part of the unmounted copy of %s at %s", entry.Source, e.Source, e.Path)
		case InTree(e.Source, entry.Source):
			return fmt.Errorf("%s holds %s, already unmounted at %s: remount it first", entry.Source, e.Source, e.Path)
		}
	}
	return nil
}
