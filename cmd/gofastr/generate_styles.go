package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// runGenerateStyles is the `gofastr generate styles [patterns]` path.
// Under the package patterns (Go-style, relative to the working
// directory, ./... by default) it first checks every <name>.tokens.css
// per program and writes <name>_tokens.gen.go beside each clean one;
// then, for every <name>.style.css, it runs the ownstyle checks
// against the theme's tokens plus the app's — judged per program, the
// same grouping `gofastr verify` uses (analyzers.CheckStylePrograms,
// one implementation) — extracts the class model, and writes
// <name>_style.gen.go. GOFASTR1816 and GOFASTR1822 run per program at
// the end: a name (or literal) two binaries never share is no finding.
// A sheet reached by several programs is generated once and validated
// against each program's tokens. It also writes .gofastr/tokens.css,
// every token for editor completion.
//
// A finding a gofastr:allow(<rule>) <reason> comment waives is not
// printed and does not block the file (ownstyle.Suppressed), the same
// marker `gofastr verify` honours.
//
// One failing file never stops the run: every diagnostic from every
// file is printed, nothing is written for a file with an
// error-severity finding, and the command exits non-zero at the end.
func runGenerateStyles(args []string) {
	if hasHelpFlag(args) {
		printGenerateStylesHelp()
		return
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fail("Unknown flag: %s (generate styles takes package patterns, not flags)", a)
			osExit(1)
			return
		}
	}
	patterns := args
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	failed := false
	files, err := discoverStyleFiles(patterns)
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	tokenFiles, err := discoverOwnedFiles(patterns, ".tokens.css")
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}

	// The program grouping `gofastr verify`'s owned-style rules judge:
	// one implementation, so the layout the docs promise (two binaries
	// in one module, each with its own copy of a siteheader package)
	// passes here because it passes there.
	srcOf := map[string]string{}
	readAll := func(fs []styleFile) []analyzers.SheetInput {
		inputs := make([]analyzers.SheetInput, 0, len(fs))
		for _, f := range fs {
			body, rerr := os.ReadFile(f.path)
			if rerr != nil {
				fail("%s: read: %v", f.rel, rerr)
				failed = true
				continue
			}
			srcOf[f.rel] = string(body)
			inputs = append(inputs, analyzers.SheetInput{Path: f.rel, Src: string(body)})
		}
		return inputs
	}
	sheetInputs := readAll(files)
	tokenInputs := readAll(tokenFiles)
	pass, perr := contracts.NewPass(".", contracts.DefaultConfig())
	if perr != nil {
		fail("read the module for program grouping: %v", perr)
		osExit(1)
		return
	}
	checks := analyzers.CheckStylePrograms(pass, sheetInputs, tokenInputs)

	appTokens, tokensFailed := generateTokenFiles(tokenFiles, srcOf, checks)
	failed = failed || tokensFailed

	// Names are unique within one program (GOFASTR1816's generator
	// arm; ownstyle.Must panics on the duplicate at init otherwise).
	// Files on both sides of a duplicate generate nothing: either
	// could be the wrong one, and generated Go holding both panics at
	// init.
	dup := map[string]bool{}
	for _, rel := range slices.Sorted(maps.Keys(checks.DuplicateNames)) {
		fail("%s: an owned style named %q already exists (%s); owned style names are unique across the program (GOFASTR1816)",
			rel, strings.TrimSuffix(filepath.Base(rel), ".style.css"),
			strings.Join(checks.DuplicateNames[rel], ", "))
		dup[rel] = true
		failed = true
	}

	// The exported var is Style for a package's only style file,
	// <Owner>Style when the package holds several.
	perDir := map[string]int{}
	for _, f := range files {
		if !dup[f.rel] {
			perDir[filepath.Dir(f.path)]++
		}
	}

	for _, f := range files {
		if dup[f.rel] {
			continue
		}
		if err := generateStyleFile(f, srcOf[f.rel], checks.SheetFindings[f.rel], perDir[filepath.Dir(f.path)] > 1); err != nil {
			failed = true
			if err != errSilent { // diagnostics were already printed
				fail("%s: %v", f.rel, err)
			}
			continue
		}
	}
	printRepeatedLiterals(srcOf, checks.RepeatedLiterals)
	if len(files) > 0 {
		info("Checked %d style file(s).", len(files))
	}
	if len(tokenFiles) > 0 {
		info("Checked %d tokens file(s).", len(tokenFiles))
	}

	if err := writeStyleTokensCSS(".", appTokens); err != nil {
		fail("write .gofastr/tokens.css: %v", err)
		failed = true
	}
	if failed {
		osExit(1)
	}
}

// errSilent marks a failure whose diagnostics were already printed
// (every CSS finding was listed; the summary only needs the exit).
var errSilent = fmt.Errorf("silent")

// styleFile is one discovered <name>.style.css.
type styleFile struct {
	path string // filesystem path
	rel  string // working-directory-relative, slash-separated, for messages
	name string // owner name (file stem)
}

// discoverStyleFiles resolves the patterns and collects every
// *.style.css under them, in deterministic (sorted) order. A file
// whose stem is not a valid owner name is an error naming the file —
// reporting it beats silently skipping it.
func discoverStyleFiles(patterns []string) ([]styleFile, error) {
	return discoverOwnedFiles(patterns, ".style.css")
}

// discoverOwnedFiles is discoverStyleFiles for any owned-file suffix:
// ".style.css" sheets and ".tokens.css" token sets share the name
// grammar and the directory walk.
func discoverOwnedFiles(patterns []string, suffix string) ([]styleFile, error) {
	dirs, err := stylePatternDirs(patterns)
	if err != nil {
		return nil, err
	}
	var out []styleFile
	for _, dir := range dirs {
		entries, derr := os.ReadDir(dir)
		if derr != nil {
			return nil, fmt.Errorf("generate styles: read %s: %w", dir, derr)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			out = append(out, styleFile{
				path: path,
				rel:  filepath.ToSlash(path),
				name: strings.TrimSuffix(e.Name(), suffix),
			})
		}
	}
	slices.SortFunc(out, func(a, b styleFile) int { return strings.Compare(a.path, b.path) })
	for _, f := range out {
		if !styleNamePattern.MatchString(f.name) || strings.HasPrefix(f.name, "ui-") {
			what := "owned style"
			if suffix == ".tokens.css" {
				what = "tokens file"
			}
			return nil, fmt.Errorf("generate styles: %s: %q is not a valid %s name: ^[a-z][a-z0-9-]*$ and not the ui- prefix; rename the file", f.rel, f.name, what)
		}
	}
	return out, nil
}

// styleNamePattern is the owner-name grammar: ^[a-z][a-z0-9-]*$, no
// ui- prefix (kit names). Mirrors ownstyle.Must's validation so the
// generator's error names the file before Must can panic.
var styleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// generateStyleFile checks, models and generates one style file.
// checkDiags are the per-program ownstyle.Check findings the shared
// grouping already computed (deduplicated across the file's programs);
// only the model diagnostics are added here. It returns errSilent
// when the failure was already reported as printed diagnostics.
func generateStyleFile(f styleFile, src string, checkDiags []ownstyle.Diagnostic, sharedPkg bool) error {
	css := src
	kind := ownstyle.KindScoped
	if f.name == "app" {
		kind = ownstyle.KindApp
	}

	diags := checkDiags
	sheet, _ := ownstyle.Parse(css) // parse errors are in checkDiags (ownstyle.Check)
	var model *ownstyle.SheetModel
	if sheet != nil {
		var modelDiags []ownstyle.Diagnostic
		model, modelDiags = ownstyle.Model(sheet)
		diags = append(slices.Clone(diags), modelDiags...)
	}
	if printDiagnostics(f.rel, css, diags) {
		return errSilent
	}

	pkg, err := stylePackageName(filepath.Dir(f.path))
	if err != nil {
		return err
	}
	out, err := ownstyle.GenerateFile(f.name, kind, css, model, pkg, sharedPkg)
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(f.path), ownstyle.GeneratedFileName(f.name))
	//gofastr:allow(worldreadable) the file is generated Go source (ownstyle.GeneratedFileName ends in .go), a public build artifact like every other generated file; 0644 is the repo's mode for generated code
	if err := os.WriteFile(dst, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	success("wrote %s", filepath.ToSlash(dst))
	return nil
}

// stylePatternDirs resolves Go-style package patterns relative to the
// working directory to the set of directories to scan: "./..." and
// "foo/..." walk their root, "foo" is the single directory. vendor/,
// testdata/, node_modules/, dot directories and _-prefixed
// directories are skipped, the same trees the go tool ignores.
func stylePatternDirs(patterns []string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" {
			dir = "."
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	for _, pat := range patterns {
		if filepath.IsAbs(pat) {
			return nil, fmt.Errorf("generate styles: %s: package patterns are relative to the working directory", pat)
		}
		root, recursive := pat, false
		if pat == "..." {
			root, recursive = ".", true
		} else if rest, ok := strings.CutSuffix(pat, "/..."); ok {
			root, recursive = rest, true
		}
		root = filepath.Clean(root)
		if root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("generate styles: %s: package patterns must stay under the working directory", pat)
		}
		if !recursive {
			info, serr := os.Stat(root)
			if serr != nil || !info.IsDir() {
				return nil, fmt.Errorf("generate styles: %s: not a directory", pat)
			}
			add(root)
			continue
		}
		werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if !d.IsDir() {
				return nil
			}
			if path != root {
				switch d.Name() {
				case "vendor", "testdata", "node_modules":
					return fs.SkipDir
				}
				if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
					return fs.SkipDir
				}
			}
			add(path)
			return nil
		})
		if werr != nil {
			return nil, fmt.Errorf("generate styles: walk %s: %w", root, werr)
		}
	}
	slices.Sort(dirs)
	return dirs, nil
}

// stylePackageName reads the Go package name from a directory's
// existing non-test .go files. A directory holding a style sheet but
// no Go source has nothing for the generated file to belong to: an
// error naming the directory.
func stylePackageName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		names = append(names, e.Name())
	}
	slices.Sort(names)
	for _, name := range names {
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if perr == nil && f.Name != nil {
			return f.Name.Name, nil
		}
	}
	return "", fmt.Errorf("directory %s holds a style sheet but no non-test Go files; a generated file must join an existing package", filepath.ToSlash(dir))
}

// writeStyleTokensCSS writes <root>/.gofastr/tokens.css: every theme
// token with its light value (and dark value, when the theme carries
// one, in a trailing comment) under :root, for editor completion in
// .style.css files, the app's own tokens included. It is a public
// build artifact, never served.
func writeStyleTokensCSS(root string, app []ownstyle.AppTokenAt) error {
	tokens := ownstyle.CheckTokens(style.ThemeToTokens(uitheme.Default()), app)
	var b strings.Builder
	b.WriteString("/* Theme tokens for editor completion. Written by `gofastr gen styles`;\n")
	b.WriteString("   never served: the running app emits its own theme. Light value\n")
	b.WriteString("   first, dark value (when the theme carries one) in the comment. */\n")
	b.WriteString(":root {\n")

	light := make([]string, 0, len(tokens))
	dark := map[string]string{}
	for k, v := range tokens {
		if rest, ok := strings.CutPrefix(k, "dark."); ok {
			dark[rest] = v
			continue
		}
		// Component options are knobs for the kit's registration path,
		// not custom properties a sheet can var(): a dotted key is not
		// even a valid name, and completion for it would be noise.
		if strings.HasPrefix(k, "component.") {
			continue
		}
		light = append(light, k)
	}
	slices.Sort(light)
	for _, k := range light {
		// The token key becomes a CSS custom-property name: a key that
		// is not one (a theme walking in a dotted component key, say)
		// is a generator bug, not a completion entry to half-emit.
		if !cssIdentOK(k) {
			return fmt.Errorf("theme token %q is not a valid CSS custom-property name", k)
		}
		line := fmt.Sprintf("  --%s: %s;", k, tokens[k])
		if d, ok := dark[k]; ok {
			line += fmt.Sprintf(" /* dark: %s */", d)
			delete(dark, k)
		}
		b.WriteString(line + "\n")
	}
	// Dark-only tokens (a dark palette entry with no light
	// counterpart) still belong in the completion list.
	var extra []string
	for k := range dark {
		extra = append(extra, k)
	}
	slices.Sort(extra)
	for _, k := range extra {
		if !cssIdentOK(k) {
			return fmt.Errorf("dark theme token %q is not a valid CSS custom-property name", k)
		}
		b.WriteString(fmt.Sprintf("  --%s: %s; /* dark-only */\n", k, dark[k]))
	}
	b.WriteString("}\n")

	dir := filepath.Join(root, ".gofastr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	dst := filepath.Join(dir, "tokens.css")
	if err := os.WriteFile(dst, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// cssIdentOK reports whether name is a valid CSS custom-property name
// segment: an identifier (letters, digits, -, _, non-ASCII), so the
// emitted `--name: value;` declaration is one declaration.
func cssIdentOK(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_':
		case r >= 0x80:
		default:
			return false
		}
	}
	return true
}

func printGenerateStylesHelp() {
	fmt.Println(`gofastr generate styles: typed Go for app tokens and owned style sheets

Usage:
  gofastr generate styles [patterns]

Under the package patterns (Go-style, relative to the working
directory; ./... by default; vendor/, testdata/, node_modules/ and dot
directories are skipped). Files are judged per program — a main
package plus the packages its imports resolve to, build constraints
per target platform — the same grouping gofastr verify uses.

Every <name>.tokens.css is checked first, one set per program:

  - it holds only @property rules (--<type>-<name>, a syntax that
    matches the type, inherits: true, an initial-value) and one
    (--dark) media block that sets dark colour values on :root,
  - a token may not reuse a built-in theme name, be declared in two
    files, or hold another token's value of the same type (GOFASTR1821),
  - <name>_tokens.gen.go is written beside a clean file: a Tokens var
    (or <Name>Tokens) of typed style values, ready for
    style.DefaultTheme().Extend(Tokens).

Then every <name>.style.css:

  - the sheet is checked (theme and app tokens, no !important, custom
    media, app-sheet selector rules; every finding is printed as
    file:line:col: severity GOFASTRnnnn message and an error stops
    that file's Go being written),
  - <name>_style.gen.go is written beside it, in the directory's
    existing Go package: the checked CSS as a constant, the
    registration (Style or <Name>Style wrapping ownstyle.Must), and
    every class as a typed method — .key is Key(), a base with
    variants gets <Base>With(<Base>Variants), a value group becomes a
    string type with Parse<Group>.

Last, a literal written in two or more sheets of one program for the
same token type is a GOFASTR1822 warning: declare it once as a token.

A comment /* gofastr:allow(GOFASTRnnnn) reason */ waives that rule on
its own line (when code precedes it) or the next line with code. The
reason is required; a bare marker waives nothing.

Also writes .gofastr/tokens.css, every theme and app token for editor
completion in .style.css files. Never served.

Run "gofastr docs cli" for the CLI map.`)
}

// printDiagnostics prints every finding no allow marker in src waives,
// as file:line:col: severity rule message, and reports whether one of
// them is an error (the file's Go must not be written).
func printDiagnostics(rel, src string, diags []ownstyle.Diagnostic) (hasErr bool) {
	for _, d := range diags {
		if ownstyle.Suppressed(src, d) {
			continue
		}
		fmt.Printf("%s:%d:%d: %s %s %s\n", rel, d.Line, d.Col, d.Severity, d.Rule, d.Message)
		if d.Severity == ownstyle.SeverityError {
			hasErr = true
		}
	}
	return hasErr
}

// generateTokenFiles writes <name>_tokens.gen.go beside each tokens
// file with no unwaived error in ANY program that reaches it (the
// shared grouping already checked each program's tokens files
// together; findings a second program would repeat are collapsed). It
// returns the union of every program's app tokens — completion and
// nothing else reads it — and whether any file failed. A file whose
// tokens parsed is in that union even when its Go was not written, so
// one typo does not cascade into GOFASTR1806 across the program.
func generateTokenFiles(files []styleFile, srcOf map[string]string, checks analyzers.StyleProgramChecks) ([]ownstyle.AppTokenAt, bool) {
	failed := false
	diagsOf := map[string][]ownstyle.Diagnostic{}
	seen := map[ownstyle.FileDiagnostic]bool{}
	parsedOf := map[string]*ownstyle.TokensFile{}
	for _, g := range checks.Groups {
		for _, d := range g.TokenFindings {
			if seen[d] {
				continue
			}
			seen[d] = true
			diagsOf[d.File] = append(diagsOf[d.File], d.Diag)
		}
		for _, rel := range slices.Sorted(maps.Keys(g.ParsedTokens)) {
			if parsedOf[rel] == nil {
				parsedOf[rel] = g.ParsedTokens[rel]
			}
		}
	}
	perDir := map[string]int{}
	for _, f := range files {
		perDir[filepath.Dir(f.path)]++
	}
	for _, f := range files {
		src, ok := srcOf[f.rel]
		if !ok {
			continue // the read failure was reported before grouping
		}
		ds := diagsOf[f.rel]
		slices.SortStableFunc(ds, func(a, b ownstyle.Diagnostic) int {
			if a.Line != b.Line {
				return a.Line - b.Line
			}
			return a.Col - b.Col
		})
		if printDiagnostics(f.rel, src, ds) {
			failed = true
			continue
		}
		pkg, err := stylePackageName(filepath.Dir(f.path))
		if err != nil {
			fail("%s: %v", f.rel, err)
			failed = true
			continue
		}
		out, err := ownstyle.GenerateTokensFile(f.name, src, parsedOf[f.rel], pkg, perDir[filepath.Dir(f.path)] > 1)
		if err != nil {
			fail("%s: %v", f.rel, err)
			failed = true
			continue
		}
		dst := filepath.Join(filepath.Dir(f.path), ownstyle.GeneratedTokensFileName(f.name))
		//gofastr:allow(worldreadable) the file is generated Go source (ownstyle.GeneratedTokensFileName ends in .go), a public build artifact like every other generated file; 0644 is the repo's mode for generated code
		if err := os.WriteFile(dst, []byte(out), 0o644); err != nil {
			fail("write %s: %v", dst, err)
			failed = true
			continue
		}
		success("wrote %s", filepath.ToSlash(dst))
	}
	var app []ownstyle.AppTokenAt
	for _, g := range checks.Groups {
		app = append(app, g.AppTokens...)
	}
	return app, failed
}

// printRepeatedLiterals prints the GOFASTR1822 warnings the shared
// grouping found (per program, merged per site). Warnings never fail
// the run.
func printRepeatedLiterals(srcOf map[string]string, found []ownstyle.FileDiagnostic) {
	for _, d := range found {
		if ownstyle.Suppressed(srcOf[d.File], d.Diag) {
			continue
		}
		fmt.Printf("%s:%d:%d: %s %s %s\n", d.File, d.Diag.Line, d.Diag.Col, d.Diag.Severity, d.Diag.Rule, d.Diag.Message)
	}
}
