package themecmd

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// RunTheme dispatches `gofastr theme <subcommand>`.
func RunTheme(args []string) {
	if len(args) == 0 {
		printThemeHelp()
		return
	}
	switch args[0] {
	case "init":
		runThemeInit(args[1:])
	case "edit":
		runThemeEdit(args[1:])
	case "--help", "-h", "help":
		printThemeHelp()
	default:
		fmt.Printf("%s Unknown theme subcommand: %s\n\n", cli.Red("✗"), args[0])
		printThemeHelp()
		cli.OsExit(1)
	}
}

func printThemeHelp() {
	fmt.Println("Usage: gofastr theme <subcommand>")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  init     Scaffold a starter theme.go in the current project.")
	fmt.Println("  edit     Launch a local theme configurator with a live preview.")
}

// runThemeInit writes a starter theme.go to the user's project. The
// generated file is the full DefaultTheme inlined as a Go file the
// user owns forever: edit values to customize, the framework never
// touches it again.
//
// Refuses to overwrite an existing file; pass --force to opt in.
func runThemeInit(args []string) {
	dest := "theme/theme.go"
	force := false
	for _, a := range args {
		switch {
		case a == "--force" || a == "-f":
			force = true
		case strings.HasPrefix(a, "--out=") || strings.HasPrefix(a, "-o="):
			dest = strings.SplitN(a, "=", 2)[1]
		case a == "--help" || a == "-h":
			fmt.Println("Usage: gofastr theme init [--out=path] [--force]")
			fmt.Println()
			fmt.Println("Writes a starter theme.go to ./theme/theme.go (override via --out).")
			fmt.Println("Refuses to overwrite an existing file unless --force is set.")
			return
		default:
			fmt.Printf("%s Unknown flag: %s\n", cli.Red("✗"), a)
			cli.OsExit(1)
		}
	}

	// dest is read and written through an os.Root (projectfs): a local
	// path is confined to the working directory, so a theme/ or theme.go
	// linked out of the checkout is refused rather than written through.
	// Lstat, not Stat: a dangling theme.go link is a file already there.
	rootDir, rel, err := projectfs.UserPath(dest)
	if err != nil {
		fmt.Printf("%s mkdir %s: %v\n", cli.Red("✗"), filepath.Dir(dest), err)
		cli.OsExit(1)
		return
	}
	if _, err := projectfs.LstatFile(rootDir, rel); err == nil && !force {
		fmt.Printf("%s %s already exists. Pass --force to overwrite.\n", cli.Red("✗"), dest)
		cli.OsExit(1)
		return
	}

	if err := projectfs.MkdirAll(rootDir, path.Dir(rel), 0o755); err != nil {
		fmt.Printf("%s mkdir %s: %v\n", cli.Red("✗"), filepath.Dir(dest), err)
		cli.OsExit(1)
		return
	}

	// The file joins whatever package already lives in its directory
	// (--out=theme.go beside main.go is package main), the way
	// `theme edit` writes back; a new directory takes its own name.
	pkg := packageNameForPath(dest)
	src := themeStarter
	if pkg != "theme" {
		src = themeStarterSource(pkg)
	}
	// --force replaces an existing theme through a temporary file, so a
	// crash mid-write leaves the old one, not an empty one.
	if err := projectfs.WriteFile(rootDir, rel, []byte(src), 0o644); err != nil {
		fmt.Printf("%s write %s: %v\n", cli.Red("✗"), dest, err)
		cli.OsExit(1)
	}

	fmt.Printf("%s wrote %s: edit values to customize your theme.\n", cli.Green("✓"), dest)
	fmt.Println()
	if pkg == "main" {
		fmt.Println("Next: the theme is in package main, so main.go uses App directly:")
		fmt.Println()
		fmt.Println("    site := app.NewApp(\"MyApp\").WithTheme(App)")
		return
	}
	importDir := filepath.ToSlash(filepath.Dir(dest))
	if filepath.IsAbs(dest) {
		importDir = pkg // an absolute --out says nothing about the module layout
	}
	fmt.Println("Next: wire it into main.go:")
	fmt.Println()
	fmt.Println("    import (")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/core-ui/app\"")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/framework\"")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/framework/uihost\"")
	fmt.Println("        \"<your-module>/" + importDir + "\"")
	fmt.Println("    )")
	fmt.Println()
	fmt.Println("    func main() {")
	fmt.Println("        site := app.NewApp(\"MyApp\").WithTheme(" + pkg + ".App)")
	fmt.Println("        site.Register(\"/\", &HomeScreen{}, nil) // HomeScreen implements component.Component + ScreenSpec")
	fmt.Println()
	fmt.Println("        host := uihost.New(site)")
	fmt.Println("        fw := framework.NewApp()")
	fmt.Println("        fw.Mount(host)              // fwApp owns the listener; host attaches its routes")
	fmt.Println("        fw.Start(\":8080\")")
	fmt.Println("    }")
	fmt.Println()
	fmt.Println("Then `go mod tidy && go run .`. Reference tokens in components")
	fmt.Println("via theme.App.Colors.Primary.CSS() (or {colors.primary} in")
	fmt.Println("StyleSheet builders). See examples/site/main.go for a full setup.")
}

// themeStarter is the file the scaffold writes: the canonical
// framework theme (framework/ui/theme.Default) through the same
// emitter `gofastr theme edit` writes back with, under the starter's
// orientation header. One source: the emitter writes every field of
// style.Theme by construction, and AutoFillNames (the emitted init)
// derives every token Name from the struct-field path, so the user
// writes only Values. The previous hand-maintained template omitted
// required fields (Colors.CodeSurface, the overlay/toast/dropdown
// durations, the easings) and a fresh scaffold panicked at boot.
var themeStarter = themeStarterSource("theme")

// starterDoc is the explanatory header `gofastr theme init` writes
// above the emitted theme.
var starterDoc = []string{
	"Every field of style.Theme is required except the optional",
	"groups (Strokes, Leading, Tracking, Opacities, Code), whose",
	"unset slots take the framework's defaults. This file was",
	"generated by `gofastr theme init` with the framework's",
	"defaults; edit values to match your brand. The framework never",
	"regenerates this file.",
	"",
	"Reference tokens at render sites:",
	"  theme.App.Colors.Primary.CSS()  → \"var(--color-primary)\"",
	"  theme.App.Colors.Primary.Value  → {{primary}} (literal)",
	"",
	"Token names auto-derive from struct field paths",
	"(Colors.PrimaryFg → --color-primary-fg) so you only write Value.",
}

func themeStarterSource(pkg string) string {
	t := uitheme.Default()
	// The app's own name, and no component options: the framework's
	// complete default set is compiled into :root anyway, so a host
	// declares Components only to deviate from it.
	t.Name = "app"
	t.Components = nil
	// The header quotes the primary this file sets, so it cannot go
	// stale when the default palette changes.
	doc := make([]string, len(starterDoc))
	for i, line := range starterDoc {
		doc[i] = strings.ReplaceAll(line, "{{primary}}", strconv.Quote(t.Colors.Primary.Value))
	}
	src, err := emitThemeGoSourceWithDoc(t, pkg, doc)
	if err != nil {
		panic("gofastr: theme starter does not parse: " + err.Error())
	}
	return string(src)
}
