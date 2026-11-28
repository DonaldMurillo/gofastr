package appgen

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"golang.org/x/mod/module"
)

var canonicalPackages = packages.FS

var packageWriteFiles = codegen.WriteFiles

// CanonicalPackageImport is the import path the packages carry inside
// this repo; a copy rewrites it to the target module's own path.
const CanonicalPackageImport = "github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/"

func runGeneratePackage(args []string) {
	if cli.HasHelpFlag(args) {
		printGeneratePackageHelp()
		return
	}
	name, outDir, dryRun, ok := parsePackageArgs(args)
	if !ok {
		cli.OsExit(2)
		return
	}
	infos, err := canonicalPackageInfos()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if name == "" {
		fmt.Println("Canonical chrome packages (copy one into your app as owned code):")
		for _, p := range infos {
			cli.Printf("  %-12s %s\n", p.Name, p.Summary)
		}
		fmt.Println()
		fmt.Println("Copy one: gofastr generate package <name> [--out=DIR] [--dry-run]")
		return
	}
	known := false
	for _, p := range infos {
		known = known || p.Name == name
	}
	if !known {
		names := make([]string, len(infos))
		for i, p := range infos {
			names[i] = p.Name
		}
		cli.Fail("unknown package %q; available: %s", name, strings.Join(names, ", "))
		cli.Info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
		cli.OsExit(2)
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	tgt, err := ResolvePackageTarget(name, outDir, wd)
	if err != nil {
		cli.Fail("%v", err)
		if errors.Is(err, ErrPackageNoModule) {
			cli.Info("Run `go mod init <path>` in your app root first, then re-run the copy from anywhere under it.")
		}
		cli.OsExit(1)
		return
	}

	if entries, derr := os.ReadDir(tgt.Dir); derr == nil && len(entries) > 0 {
		cli.Fail("%s already exists and is not empty: `generate package` is a one-shot copy and the app owns the result; move the directory away first if you really want to replace it", tgt.Display)
		cli.OsExit(1)
		return
	}

	source := CanonicalPackageImport + name
	settingsPath, entries, err := project.UnmountedSettings(tgt.AnchorDir)
	if err == nil {
		err = CheckUnmountOverlap(settings.Unmounted{Source: source, Path: filepath.ToSlash(tgt.Root)}, entries, false)
	}
	entry := settings.Unmounted{Source: source, Version: MarkerVersion(project.CliVersion()), Path: filepath.ToSlash(tgt.Root), Date: UnmountNow().Format(time.DateOnly)}
	if err == nil {
		err = entry.Validate()
	}
	var files []GeneratedFile
	var hasTokens, hasChromiumTest bool
	if err == nil {
		files, hasTokens, hasChromiumTest, err = copyCanonicalPackage(name, tgt.ImportPath)
	}
	if err == nil {
		tree := UnmountTree{Source: source, Version: entry.Version}
		for _, f := range files {
			tree.Files = append(tree.Files, PackageFile{Name: f.Name, Content: f.Content})
		}
		files, err = BuildUnmountCopy(tree, tgt.ImportPath)
	}
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if dryRun {
		fmt.Println("Would write:")
		for _, f := range files {
			cli.Printf("  %s\n", filepath.ToSlash(filepath.Join(tgt.Display, f.Name)))
		}
		cli.Printf("Would record it in %s\n", settingsPath)
		return
	}
	undoCopy, err := WritePackageCopy(tgt, files)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if err := settings.SetUnmounted(settingsPath, append(slices.Clone(entries), entry)); err != nil {
		undoCopy()
		cli.Fail("record the copy: %v; the copy was removed", err)
		cli.OsExit(1)
		return
	}
	cli.Success("Copied %s into %s (%d file(s)) and recorded it in %s; the package is yours now — edit it freely", name, tgt.Display, len(files), settings.FileName)
	fmt.Println()
	fmt.Println("  Next steps:")
	if hasTokens {
		cli.Printf("    site.WithTheme(theme.Default().Extend(%s.Tokens)) : add the package's own tokens to your theme\n", name)
	}
	if hasChromiumTest {
		fmt.Println("    go mod tidy          : the chromium test pulls github.com/chromedp/chromedp and github.com/chromedp/cdproto")
	}
	generated := name + "_style.gen.go"
	if hasTokens {
		generated += " / " + name + "_tokens.gen.go"
	}
	cli.Printf("    gofastr gen styles   : regenerate %s after editing the sheets\n", generated)
}

// ErrPackageNoModule is the error for a run outside any module; the caller prints its own hint.
var ErrPackageNoModule = errors.New("no enclosing go.mod: the copied package imports itself by module path, so it cannot build until this directory is a Go module")

type packageTarget struct {
	Display    string
	Dir        string
	ImportPath string
	AnchorDir  string
	Root       string
}

// ResolvePackageTarget resolves --out (default ./<name>) against wd, inside wd's module, taking the import path from the go.mod enclosing the target.
func ResolvePackageTarget(name, outDir, wd string) (packageTarget, error) {
	display := outDir
	if display == "" {
		display = name
	}
	dir := display
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(wd, dir)
	}
	dir = filepath.Clean(dir)

	modulePath, moduleRoot := project.FindEnclosingGoMod(wd)
	if modulePath == "" {
		return packageTarget{}, ErrPackageNoModule
	}
	outside := fmt.Errorf("target %s sits outside the module rooted at %s, so the package's import path cannot be derived from it", display, moduleRoot)
	if rel, err := filepath.Rel(moduleRoot, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return packageTarget{}, outside
	}

	enclosingPath, enclosingDir := project.FindEnclosingGoMod(dir)
	if enclosingPath == "" {
		return packageTarget{}, outside
	}
	root, err := filepath.Rel(enclosingDir, dir)
	if err != nil {
		return packageTarget{}, outside
	}
	importPath := enclosingPath
	if root != "." {
		importPath += "/" + filepath.ToSlash(root)
	}
	return packageTarget{
		Display:    display,
		Dir:        dir,
		ImportPath: importPath,
		AnchorDir:  enclosingDir,
		Root:       root,
	}, nil
}

func packageDirChainExisted(tgt packageTarget) map[string]bool {
	existed := map[string]bool{}
	if codegen.EnsureNoSymlinkPath(tgt.Dir) != nil {
		existed[tgt.Dir] = true
		return existed
	}
	for d := tgt.Dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			existed[d] = true
		}
		if d == tgt.AnchorDir || d == filepath.Dir(d) {
			return existed
		}
	}
}

func rollbackPackageCopy(tgt packageTarget, files []GeneratedFile, existed map[string]bool) (filesRemoved, dirsRemoved int) {
	for _, f := range files {
		p := filepath.Join(tgt.Dir, filepath.FromSlash(f.Name))
		if codegen.EnsureNoSymlinkPath(p) != nil {
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil || string(body) != f.Content {
			continue
		}
		if err := os.Remove(p); err == nil {
			filesRemoved++
		}
	}
	var subdirs []string
	seen := map[string]bool{}
	for _, f := range files {
		for d := filepath.Dir(filepath.Join(tgt.Dir, filepath.FromSlash(f.Name))); d != tgt.Dir && !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			subdirs = append(subdirs, d)
		}
	}
	slices.SortFunc(subdirs, func(a, b string) int { return len(b) - len(a) })
	for _, d := range subdirs {
		if codegen.EnsureNoSymlinkPath(d) != nil {
			continue
		}
		if err := os.Remove(d); err == nil {
			dirsRemoved++
		}
	}
	for d := tgt.Dir; ; d = filepath.Dir(d) {
		if d == tgt.AnchorDir || existed[d] {
			break
		}
		if codegen.EnsureNoSymlinkPath(d) != nil {
			break
		}
		if err := os.Remove(d); err != nil {
			break
		}
		dirsRemoved++
	}
	return filesRemoved, dirsRemoved
}

const packageUsage = "Usage: gofastr generate package <name> [--out=DIR] [--dry-run]"

func parsePackageArgs(args []string) (name, outDir string, dryRun, ok bool) {
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dry-run":
			dryRun = true
		case strings.HasPrefix(arg, "--out="):
			outDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--out":
			if i+1 >= len(args) || cli.IsFlagToken(args[i+1]) {
				failGenerateArgs(args, cli.NeedsValue("--out"), packageUsage)
				return "", "", false, false
			}
			i++
			outDir = args[i]
		case arg == "--force", arg == "--add", arg == "--json":
			failGenerateArgs(args, fmt.Errorf("%q is not supported on `gofastr generate package`: the copy is one-shot (it refuses a non-empty target and never overwrites existing files)", arg), packageUsage)
			return "", "", false, false
		case strings.HasPrefix(arg, "--"):
			failGenerateArgs(args, fmt.Errorf("Unknown flag %q for `gofastr generate package`", arg), packageUsage)
			return "", "", false, false
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) > 1 {
		failGenerateArgs(args, fmt.Errorf("`gofastr generate package` takes at most one name argument (none lists the available packages)"), packageUsage)
		return "", "", false, false
	}
	if len(positionals) == 1 {
		name = positionals[0]
	}
	return name, outDir, dryRun, true
}

func printGeneratePackageHelp() {
	fmt.Println(`gofastr generate package: copy canonical chrome into your app

Usage:
  gofastr generate package            List the available packages
  gofastr generate package <name> [--out=<dir>] [--dry-run]

Copies one canonical package (siteheader, sitefooter, docpage) into
<dir> (default ./<name>) as the app's own code: the .go files, the
owned .style.css sheet, its .tokens.css when the package has one, the
generated _style.gen.go / _tokens.gen.go, and the tests. The package's
self-import is rewritten to your module's path, so run it inside a
go.mod. The copy is one-shot: a non-empty target is refused, and after
the copy the package is yours to edit (gofastr gen styles regenerates
the .gen.go files after you edit a sheet).`)
}

type canonicalPackageInfo struct {
	Name    string
	Summary string
}

func canonicalPackageInfos() ([]canonicalPackageInfo, error) {
	entries, err := fs.ReadDir(canonicalPackages, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded packages: %w", err)
	}
	var out []canonicalPackageInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := readEmbeddedPackage(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, canonicalPackageInfo{
			Name:    e.Name(),
			Summary: packageDocSummary(e.Name(), files),
		})
	}
	slices.SortFunc(out, func(a, b canonicalPackageInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// PackageFile is one file of an embedded package, base name only.
type PackageFile struct {
	Name    string
	Content string
}

func readEmbeddedPackage(name string) ([]PackageFile, error) {
	entries, err := fs.ReadDir(canonicalPackages, name)
	if err != nil {
		return nil, fmt.Errorf("read embedded package %s: %w", name, err)
	}
	var out []PackageFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := canonicalPackages.ReadFile(name + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded package %s: %w", name, err)
		}
		out = append(out, PackageFile{Name: e.Name(), Content: string(body)})
	}
	slices.SortFunc(out, func(a, b PackageFile) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func copyCanonicalPackage(name, targetImport string) (files []GeneratedFile, hasTokens, hasChromiumTest bool, err error) {
	if err := module.CheckImportPath(targetImport); err != nil {
		return nil, false, false, fmt.Errorf("cannot copy %s: %w", name, err)
	}
	src, err := readEmbeddedPackage(name)
	if err != nil {
		return nil, false, false, err
	}
	byName := make(map[string]string, len(src))
	for _, f := range src {
		byName[f.Name] = f.Content
	}
	if err := checkCanonicalPackageSheets(name, byName); err != nil {
		return nil, false, false, err
	}
	for _, f := range src {
		content := f.Content
		if strings.HasSuffix(f.Name, ".go") {
			rewritten, _, rerr := RewriteImportPrefix(content, CanonicalPackageImport+name, targetImport)
			if rerr != nil {
				return nil, false, false, fmt.Errorf("%s/%s: %w", name, f.Name, rerr)
			}
			content = FormatGenerated(f.Name, rewritten)
		}
		files = append(files, GeneratedFile{Name: f.Name, Content: content})
		hasTokens = hasTokens || f.Name == name+".tokens.css"
		hasChromiumTest = hasChromiumTest || strings.HasSuffix(f.Name, "_chromium_test.go")
	}
	return files, hasTokens, hasChromiumTest, nil
}

func checkCanonicalPackageSheets(name string, files map[string]string) error {
	builtins := style.ThemeToTokens(style.DefaultTheme())
	rel := func(file string) string { return name + "/" + file }

	var appTokens []ownstyle.AppTokenAt
	if tokensSrc, ok := files[name+".tokens.css"]; ok {
		src := ownstyle.SheetSource{File: rel(name + ".tokens.css"), Src: tokensSrc}
		_, tokens, tokenDiags := ownstyle.CheckTokenFiles([]ownstyle.SheetSource{src}, builtins)
		for _, d := range tokenDiags {
			if err := canonicalPackageSheetFinding(d.File, tokensSrc, d.Diag); err != nil {
				return err
			}
		}
		appTokens = tokens
	}
	tokens := ownstyle.CheckTokens(builtins, appTokens)

	css, ok := files[name+".style.css"]
	if !ok {
		return nil
	}
	sheetRel := rel(name + ".style.css")
	diags := ownstyle.Check(sheetRel, css, ownstyle.KindScoped, tokens)
	sheet, parseDiags := ownstyle.Parse(css)
	diags = append(diags, parseDiags...)
	if sheet != nil {
		_, modelDiags := ownstyle.Model(sheet)
		diags = append(diags, modelDiags...)
	}
	for _, d := range diags {
		if err := canonicalPackageSheetFinding(sheetRel, css, d); err != nil {
			return err
		}
	}
	return nil
}

func canonicalPackageSheetFinding(rel, src string, d ownstyle.Diagnostic) error {
	if ownstyle.Suppressed(src, d) {
		return nil
	}
	return fmt.Errorf("%s:%d:%d: %s %s %s", rel, d.Line, d.Col, d.Severity, d.Rule, d.Message)
}

func packageDocSummary(name string, files []PackageFile) string {
	for _, f := range files {
		if f.Name != name+".go" {
			continue
		}
		fset := token.NewFileSet()
		pf, err := parser.ParseFile(fset, f.Name, f.Content, parser.ParseComments)
		if err != nil || pf.Doc == nil {
			return ""
		}
		return firstDocSentence(pf.Doc.Text(), name)
	}
	return ""
}

func firstDocSentence(text, pkg string) string {
	para := text
	if before, _, found := strings.Cut(text, "\n\n"); found {
		para = before
	}
	joined := strings.Join(strings.Fields(para), " ")
	joined = strings.TrimPrefix(joined, "Package "+pkg+" ")
	for i := range joined {
		c := joined[i]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		if i+1 == len(joined) || joined[i+1] == ' ' {
			return joined[:i+1]
		}
	}
	return joined
}
