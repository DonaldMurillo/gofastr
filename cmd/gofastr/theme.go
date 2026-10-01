package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// runTheme dispatches `gofastr theme <subcommand>`.
func runTheme(args []string) {
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
		fmt.Printf("%s Unknown theme subcommand: %s\n\n", red("✗"), args[0])
		printThemeHelp()
		osExit(1)
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
			fmt.Printf("%s Unknown flag: %s\n", red("✗"), a)
			osExit(1)
		}
	}

	if _, err := os.Stat(dest); err == nil && !force {
		fmt.Printf("%s %s already exists. Pass --force to overwrite.\n", red("✗"), dest)
		osExit(1)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fmt.Printf("%s mkdir %s: %v\n", red("✗"), filepath.Dir(dest), err)
		osExit(1)
	}

	if err := os.WriteFile(dest, []byte(themeStarter), 0o644); err != nil {
		fmt.Printf("%s write %s: %v\n", red("✗"), dest, err)
		osExit(1)
	}

	fmt.Printf("%s wrote %s: edit values to customize your theme.\n", green("✓"), dest)
	fmt.Println()
	fmt.Println("Next: wire it into main.go:")
	fmt.Println()
	fmt.Println("    import (")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/core-ui/app\"")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/framework\"")
	fmt.Println("        \"github.com/DonaldMurillo/gofastr/framework/uihost\"")
	fmt.Println("        \"<your-module>/theme\"")
	fmt.Println("    )")
	fmt.Println()
	fmt.Println("    func main() {")
	fmt.Println("        site := app.NewApp(\"MyApp\").WithTheme(theme.App)")
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

// themeStarter is the file the scaffold writes: the canonical framework
// theme (framework/ui/theme.Default), light and dark, emitted by the same
// writer `gofastr theme edit` saves through. Generating it rather than
// keeping a hand-written copy means a token added to style.Theme is in
// every new starter, and the starter can never fail Validate at boot.
// Token Names are left to style.AutoFillNames (Colors.Primary →
// --color-primary). Component options stay out: the framework compiles
// its complete default set into :root anyway, so a starter declares
// Components only to deviate.
var themeStarter = themeStarterSource()

func themeStarterSource() string {
	t := uitheme.Default()
	t.Name = "app"
	t.Components = nil
	src, err := emitThemeGo(t, "theme", "gofastr theme init")
	if err != nil {
		panic("gofastr: theme starter does not format: " + err.Error())
	}
	return string(src)
}
