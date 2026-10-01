package main

import (
	"embed"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"golang.org/x/mod/module"
)

// canonicalPackages is the copyable chrome: siteheader, sitefooter and
// docpage, the packages an app owns when the framework does not ship
// the surface. The directory is the single source of truth — this
// embed is how the binary ships it, and `gofastr generate package`
// plus the blueprint's marketing chrome both copy FROM it, so no
// second copy or template fork can drift.
//
//go:embed packages
var canonicalPackages embed.FS

// canonicalPackageImport is the import path the packages carry inside
// this repo; a copy rewrites it to the target module's own path.
const canonicalPackageImport = "github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/"

// runGeneratePackage is the `gofastr generate package [<name>]` path:
// with no name it lists the canonical chrome packages with a one-line
// summary each; with a name it copies that package into the app as
// owned code, rewriting the package's self-import to the target
// module's path. One-shot: the app owns the result, so a non-empty
// target is refused rather than merged into.
func runGeneratePackage(args []string) {
	if hasHelpFlag(args) {
		printGeneratePackageHelp()
		return
	}
	name, outDir, dryRun, ok := parsePackageArgs(args)
	if !ok {
		osExit(1)
		return
	}
	infos, err := canonicalPackageInfos()
	if err != nil {
		fail("%v", err)
		osExit(1)
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
		fail("unknown package %q; available: %s", name, strings.Join(names, ", "))
		info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
		osExit(1)
		return
	}

	// The copied code imports itself by module path, so an enclosing
	// go.mod is a requirement, not a convenience. Mirrors generate cli.
	wd, err := os.Getwd()
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	modulePath, moduleRoot := findEnclosingGoMod(wd)
	if modulePath == "" {
		fail("no enclosing go.mod: the copied package imports itself by module path, so it cannot build until this directory is a Go module")
		info("Run `go mod init <path>` in your app root first, then re-run the copy from anywhere under it.")
		osExit(1)
		return
	}
	target := outDir
	if target == "" {
		target = name
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	rel, err := filepath.Rel(moduleRoot, absTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fail("target %s sits outside the module rooted at %s, so the package's import path cannot be derived from it", target, moduleRoot)
		osExit(1)
		return
	}
	importPath := modulePath
	if rel != "." {
		importPath += "/" + filepath.ToSlash(rel)
	}

	// One-shot copy: the app owns the result. An existing non-empty
	// target means someone's code is already there; there is no merge
	if entries, derr := os.ReadDir(target); derr == nil && len(entries) > 0 {
		fail("%s already exists and is not empty: `generate package` is a one-shot copy and the app owns the result; move the directory away first if you really want to replace it", target)
		osExit(1)
		return
	}

	files, hasTokens, hasChromiumTest, err := copyCanonicalPackage(name, importPath)
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	if dryRun {
		fmt.Println("Would write:")
		for _, f := range files {
			fmt.Printf("  %s\n", filepath.ToSlash(filepath.Join(target, f.name)))
		}
		return
	}
	fileSet := codegen.NewFileSet()
	for _, f := range files {
		if err := fileSet.Add(codegen.GeneratedFile{Path: f.name, Content: f.content}); err != nil {
			fail("%v", err)
			osExit(1)
			return
		}
	}
	if err := codegen.WriteFiles(fileSet, codegen.WriteOptions{
		OutputRoot:   target,
		SkipManifest: true,
		Conflict:     codegen.ConflictOverwrite,
	}); err != nil {
		fail("Failed to write package files: %v", err)
		osExit(1)
		return
	}
	success("Copied %s into %s (%d file(s)); the package is yours now — edit it freely", name, target, len(files))
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
				fail("--out requires a directory argument")
				info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
				return "", "", false, false
			}
			i++
			outDir = args[i]
		case arg == "--force", arg == "--add", arg == "--json":
			fail("%q is not supported on `gofastr generate package`: the copy is one-shot (it refuses a non-empty target and never overwrites existing files)", arg)
			info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
			return "", "", false, false
		case strings.HasPrefix(arg, "--"):
			fail("Unknown flag %q for `gofastr generate package`", arg)
			info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
			return "", "", false, false
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) > 1 {
		fail("`gofastr generate package` takes at most one name argument (none lists the available packages)")
		info("Usage: gofastr generate package <name> [--out=DIR] [--dry-run]")
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
	entries, err := fs.ReadDir(canonicalPackages, "packages")
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

// packageFile is one file of an embedded package, base name only.
type packageFile struct {
	Name    string
	Content string
}

// readEmbeddedPackage reads every file of the named package from the
// embedded tree, sorted by name so the copy order is deterministic.
func readEmbeddedPackage(name string) ([]packageFile, error) {
	entries, err := fs.ReadDir(canonicalPackages, "packages"+"/"+name)
	if err != nil {
		return nil, fmt.Errorf("read embedded package %s: %w", name, err)
	}
	var out []packageFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := canonicalPackages.ReadFile("packages" + "/" + name + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded package %s: %w", name, err)
		}
		out = append(out, packageFile{Name: e.Name(), Content: string(body)})
	}
	slices.SortFunc(out, func(a, b packageFile) int { return strings.Compare(a.Name, b.Name) })
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
func copyCanonicalPackage(name, targetImport string) (files []generatedFile, hasTokens, hasChromiumTest bool, err error) {
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
			rewritten, _, rerr := rewritePackageImport(content, canonicalPackageImport+name, targetImport)
			if rerr != nil {
				return nil, false, false, fmt.Errorf("%s/%s: %w", name, f.Name, rerr)
			}
			content = formatGenerated(f.Name, rewritten)
		}
		files = append(files, generatedFile{name: f.Name, content: content})
		hasTokens = hasTokens || f.Name == name+".tokens.css"
		hasChromiumTest = hasChromiumTest || strings.HasSuffix(f.Name, "_chromium_test.go")
	}
	return files, hasTokens, hasChromiumTest, nil
}

// checkCanonicalPackageSheets runs the ownstyle checks over one
// package's tokens file (if any) and style sheet, exactly as `gofastr
// gen styles` and the app's `gofastr verify` will judge the copies: an
// unwaived finding is an error, so a dirty canonical package fails
// every copy and the blueprint instead of shipping an app whose verify
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

// rewritePackageImport replaces the import path from with to in a Go
// source file by splicing the import-spec path token the parser
// located — an exact import-spec replacement, never a blind substring
// replace over the whole file (a matching string in a comment, a
// string literal or a selector would corrupt the copy).
func rewritePackageImport(src, from, to string) (string, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return "", false, fmt.Errorf("parse imports: %w", err)
	}
	var out []byte
	changed := false
	off := 0
	for _, spec := range f.Imports {
		path, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil || path != from {
			continue
		}
		start := fset.Position(spec.Path.Pos()).Offset
		end := fset.Position(spec.Path.End()).Offset
		out = append(out, src[off:start]...)
		out = append(out, strconv.Quote(to)...)
		off = end
		changed = true
	}
	out = append(out, src[off:]...)
	return string(out), changed, nil
}

// packageDocSummary returns the first sentence of the package doc
// comment from the package's own <name>.go, with the conventional
// "Package <name>" prefix dropped: the one-line summary list mode
// prints. Empty when the file is missing or carries no doc comment.
func packageDocSummary(name string, files []packageFile) string {
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
