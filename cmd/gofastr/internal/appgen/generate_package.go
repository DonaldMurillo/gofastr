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

// canonicalPackages is the copyable chrome: siteheader, sitefooter and
// docpage, the packages an app owns when the framework does not ship
// the surface. The directory is the single source of truth — its embed
// (cmd/gofastr/packages) is how the binary ships it, and `gofastr
// generate package` plus the app spec's marketing chrome both copy FROM
// it, so no second copy or template fork can drift.
var canonicalPackages = packages.FS

// packageWriteFiles writes a package copy's files; a var so tests can
// interpose exactly where a concurrent process can race the copy — after
// the one-shot emptiness check, before the first file lands. The default
// is the codegen writer the real copy uses.
var packageWriteFiles = codegen.WriteFiles

// CanonicalPackageImport is the import path the packages carry inside
// this repo; a copy rewrites it to the target module's own path.
const CanonicalPackageImport = "github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/"

// runGeneratePackage is the `gofastr generate package [<name>]` path:
// with no name it lists the canonical chrome packages with a one-line
// summary each; with a name it copies that package into the app as
// owned code, rewriting the package's self-import to the target
// module's path. One-shot: the app owns the result, so a non-empty
// target is refused rather than merged into.
func runGeneratePackage(args []string) {
	if cli.HasHelpFlag(args) {
		printGeneratePackageHelp()
		return
	}
	name, outDir, dryRun, ok := parsePackageArgs(args)
	if !ok {
		cli.OsExit(1)
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
			fmt.Printf("  %-12s %s\n", p.Name, p.Summary)
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
		cli.OsExit(1)
		return
	}

	// The copied code imports itself by module path, so an enclosing
	// go.mod is a requirement, not a convenience. Mirrors generate cli.
	// The target is resolved once, here, into everything both --dry-run
	// and the real copy need — so the two modes can never disagree about
	// which targets are acceptable.
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

	// One-shot copy: the app owns the result. An existing non-empty
	// target means someone's code is already there; there is no merge
	if entries, derr := os.ReadDir(tgt.Dir); derr == nil && len(entries) > 0 {
		cli.Fail("%s already exists and is not empty: `generate package` is a one-shot copy and the app owns the result; move the directory away first if you really want to replace it", tgt.Display)
		cli.OsExit(1)
		return
	}

	// The copy is an unmount of the canonical package from the embedded
	// tree: same markers, same hook, same settings entry, so verify,
	// upgrade and remount see it like any other copy.
	source := CanonicalPackageImport + name
	settingsPath, entries, err := UnmountedEntries(tgt.AnchorDir)
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
			fmt.Printf("  %s\n", filepath.ToSlash(filepath.Join(tgt.Display, f.Name)))
		}
		fmt.Printf("Would record it in %s\n", settingsPath)
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
		fmt.Printf("    site.WithTheme(theme.Default().Extend(%s.Tokens)) : add the package's own tokens to your theme\n", name)
	}
	if hasChromiumTest {
		fmt.Println("    go mod tidy          : the chromium test pulls github.com/chromedp/chromedp and github.com/chromedp/cdproto")
	}
	generated := name + "_style.gen.go"
	if hasTokens {
		generated += " / " + name + "_tokens.gen.go"
	}
	fmt.Printf("    gofastr gen styles   : regenerate %s after editing the sheets\n", generated)
}

// ErrPackageNoModule names the missing-module failure; the caller prints
// its own hint for it. Every other resolution failure carries a
// self-explanatory message.
var ErrPackageNoModule = errors.New("no enclosing go.mod: the copied package imports itself by module path, so it cannot build until this directory is a Go module")

// packageTarget is a copy destination resolved once, so --dry-run and the
// real copy judge exactly the same targets: display is the target as the
// user spelled it (what every message prints), dir its absolute path,
// importPath the module path the copied code will import itself by,
// anchorDir the directory the write runs from, and root the output root
// relative to anchorDir — a relative path with no parent traversal, the
// only form the codegen writer accepts.
type packageTarget struct {
	Display    string
	Dir        string
	ImportPath string
	AnchorDir  string
	Root       string
}

// ResolvePackageTarget resolves --out (default ./<name>) from the working
// directory. The command runs inside a module and may only copy within
// it; inside that fence any spelling works, including one that escapes
// the working directory ("../chrome/hdr" from a subdirectory) or an
// absolute path — both previously approved by --dry-run and then refused
// by the writer. The import path comes from the go.mod that encloses the
// TARGET, not the working directory: a nested module (tools/go.mod)
// owns the packages under it, and a path glued onto the outer module
// would not compile there.
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

	// Safe by the fence above, but keep the failure honest if the walk
	// ever disagrees: an import path must never be guessed.
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

// packageDirChainExisted snapshots which directories on the anchor→target
// chain exist, taken BEFORE the copy writes: rollback may remove a
// directory only when this map says the copy itself created it.
func packageDirChainExisted(tgt packageTarget) map[string]bool {
	existed := map[string]bool{}
	if codegen.EnsureNoSymlinkPath(tgt.Dir) != nil {
		// A symlinked chain is not the copy's to reason about; record
		// everything as pre-existing so rollback removes no directory.
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

// rollbackPackageCopy removes what a failed copy created in the target:
// every file whose current bytes are exactly the copy's own output (the
// target was verified empty before the write began, so matching bytes are
// this run's), then the directories the run created along the
// anchor→target chain (per the existed snapshot taken before the write),
// deepest first, stopping at the first directory that existed before or
// still holds something. A file another process dropped in mid-copy —
// the refusal that failed the copy — does not match and survives; a
// symlink planted on a path stops the rollback there (Remove never
// follows one, and the guard keeps the walk from touching anything it
// cannot see through).
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
	// Directories the copy created inside the target, deepest first. The
	// target was empty, so a directory there is the copy's unless a racer
	// filled it, and os.Remove refuses a directory that is not empty.
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

// parsePackageArgs parses the flags accepted by `generate package
// [<name>]`: at most one positional (no name means list mode), plus
// --out and --dry-run. --force, --add and --json are rejected (the
// copy is one-shot and owns no machine-readable shape), and any extra
// positional or unknown flag fails with usage guidance. ok is false
// (after printing the failure) when the arguments are invalid.
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
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				cli.Fail("--out requires a directory argument")
				cli.Info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
				return "", "", false, false
			}
			i++
			outDir = args[i]
		case arg == "--force", arg == "--add", arg == "--json":
			cli.Fail("%q is not supported on `gofastr generate package`: the copy is one-shot (it refuses a non-empty target and never overwrites existing files)", arg)
			cli.Info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
			return "", "", false, false
		case strings.HasPrefix(arg, "--"):
			cli.Fail("Unknown flag %q for `gofastr generate package`", arg)
			cli.Info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
			return "", "", false, false
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) > 1 {
		cli.Fail("`gofastr generate package` takes at most one name argument (none lists the available packages)")
		cli.Info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
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

// canonicalPackageInfo is one row of the list mode: a package name and
// the first sentence of its package doc comment.
type canonicalPackageInfo struct {
	Name    string
	Summary string
}

// canonicalPackageInfos lists the embedded packages in name order with
// a one-line summary each, parsed from the package doc comment — not a
// hand-maintained table, so a new package under packages/ appears here
// (and in the command) with no second list to update.
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

// readEmbeddedPackage reads every file of the named package from the
// embedded tree, sorted by name so the copy order is deterministic.
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

// copyCanonicalPackage copies the named package for a target import
// path: the sheets are checked with the same ownstyle rules the app's
// `gofastr verify` runs (a finding is a bug in the canonical package,
// not in the app, and fails the copy instead of shipping), every .go
// file's import of the canonical path is rewritten to targetImport
// through the parser-located import spec, and the Go is gofmt'd so the
// bytes an app receives are clean whatever the import sort order does
// to them. Returns the files (base names), whether the package carries
// a .tokens.css, and whether it carries a chromium test.
func copyCanonicalPackage(name, targetImport string) (files []GeneratedFile, hasTokens, hasChromiumTest bool, err error) {
	// The import path lands in Go source, so it must be one: a target
	// directory named with a quote or a space would otherwise emit a
	// file that does not parse.
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
			// Rewriting from == to is the identity; no special case.
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

// checkCanonicalPackageSheets runs the ownstyle checks over one
// package's tokens file (if any) and style sheet, exactly as `gofastr
// gen styles` and the app's `gofastr verify` will judge the copies: an
// unwaived finding is an error, so a dirty canonical package fails
// every copy and the app spec instead of shipping an app whose verify
// is red by construction.
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

// canonicalPackageSheetFinding returns an error for one unwaived
// finding in a canonical package sheet, nil when a gofastr:allow
// marker in the sheet waives it. Warnings fail too: the sheets are
// fixed source the repo owns, so there is no "address it later".
func canonicalPackageSheetFinding(rel, src string, d ownstyle.Diagnostic) error {
	if ownstyle.Suppressed(src, d) {
		return nil
	}
	return fmt.Errorf("%s:%d:%d: %s %s %s", rel, d.Line, d.Col, d.Severity, d.Rule, d.Message)
}

// packageDocSummary returns the first sentence of the package doc
// comment from the package's own <name>.go, with the conventional
// "Package <name>" prefix dropped: the one-line summary list mode
// prints. Empty when the file is missing or carries no doc comment.
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

// firstDocSentence reduces a package doc comment to its first
// sentence: the first paragraph, whitespace-collapsed, cut at the
// first sentence terminator followed by a space or the end (a period
// inside siteheader.style.css or _style.gen.go is followed by more
// identifier bytes and does not cut).
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
