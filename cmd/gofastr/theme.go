package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	case "--help", "-h", "help":
		printThemeHelp()
	default:
		fmt.Printf("%s Unknown theme subcommand: %s\n\n", red("✗"), args[0])
		printThemeHelp()
		os.Exit(1)
	}
}

func printThemeHelp() {
	fmt.Println("Usage: gofastr theme <subcommand>")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  init     Scaffold a starter theme.go in the current project.")
}

// runThemeInit writes a starter theme.go to the user's project. The
// generated file is the full DefaultTheme inlined as a Go file the
// user owns forever — edit values to customize, the framework never
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
			os.Exit(1)
		}
	}

	if _, err := os.Stat(dest); err == nil && !force {
		fmt.Printf("%s %s already exists. Pass --force to overwrite.\n", red("✗"), dest)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fmt.Printf("%s mkdir %s: %v\n", red("✗"), filepath.Dir(dest), err)
		os.Exit(1)
	}

	if err := os.WriteFile(dest, []byte(themeStarter), 0o644); err != nil {
		fmt.Printf("%s write %s: %v\n", red("✗"), dest, err)
		os.Exit(1)
	}

	fmt.Printf("%s wrote %s — edit values to customize your theme.\n", green("✓"), dest)
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  • Import the package and pass theme.App to your UIHost:")
	fmt.Println("      app.WithTheme(theme.App)")
	fmt.Println("  • Reference tokens in components via theme.App.Colors.Primary.CSS()")
	fmt.Println("    (or {colors.primary} in StyleSheet builders).")
}

// themeStarter is the file the scaffold writes. Fully-populated copy
// of style.DefaultTheme() so the user has a working baseline — they
// edit values, never wonder what fields to provide.
const themeStarter = `// Package theme defines this app's typed design system.
//
// Every field of style.Theme is required — the framework expects
// every primitive token to be populated. This file was generated
// by ` + "`gofastr theme init`" + ` with the framework's default
// values; edit them to match your brand. The framework never
// regenerates this file once it exists.
//
// Reference tokens at render sites via theme.App.Colors.Primary.CSS()
// or, inside a style.StyleSheet builder, "{colors.primary}". Both
// emit ` + "`var(--color-primary)`" + ` — section-level theme
// overrides cascade automatically via CSS variables.
package theme

import (
	"time"

	"github.com/gofastr/gofastr/core-ui/style"
)

// App is the app's design system. Pass it to the host:
//
//	app.WithTheme(theme.App)
var App = style.Theme{
	Name: "app",
	Colors: style.ColorSet{
		Primary:      style.Color{Name: "primary", Value: "#4F46E5"},
		PrimaryFg:    style.Color{Name: "primary-fg", Value: "#FFFFFF"},
		Secondary:    style.Color{Name: "secondary", Value: "#6B7280"},
		SecondaryFg:  style.Color{Name: "secondary-fg", Value: "#FFFFFF"},
		Background:   style.Color{Name: "background", Value: "#F9FAFB"},
		Surface:      style.Color{Name: "surface", Value: "#FFFFFF"},
		SurfaceSoft:  style.Color{Name: "surface-soft", Value: "#F4F4F5"},
		Text:         style.Color{Name: "text", Value: "#18181B"},
		TextMuted:    style.Color{Name: "text-muted", Value: "#52525B"},
		TextSubtle:   style.Color{Name: "text-subtle", Value: "#A1A1AA"},
		Border:       style.Color{Name: "border", Value: "#E4E4E7"},
		BorderStrong: style.Color{Name: "border-strong", Value: "#A1A1AA"},
		Danger:       style.Color{Name: "danger", Value: "#DC2626"},
		Success:      style.Color{Name: "success", Value: "#16A34A"},
		Warning:      style.Color{Name: "warning", Value: "#CA8A04"},
		Info:         style.Color{Name: "info", Value: "#2563EB"},
		Accent:       style.Color{Name: "accent", Value: "#7C3AED"},
	},
	Spacing: style.SpacingScale{
		XS:   style.Spacing{Name: "xs", Value: 2},
		SM:   style.Spacing{Name: "sm", Value: 4},
		MD:   style.Spacing{Name: "md", Value: 8},
		LG:   style.Spacing{Name: "lg", Value: 16},
		XL:   style.Spacing{Name: "xl", Value: 24},
		XXL:  style.Spacing{Name: "2xl", Value: 32},
		XXXL: style.Spacing{Name: "3xl", Value: 48},
	},
	Radii: style.RadiusSet{
		None: style.Radius{Name: "none", Value: 0},
		SM:   style.Radius{Name: "sm", Value: 4},
		MD:   style.Radius{Name: "md", Value: 8},
		LG:   style.Radius{Name: "lg", Value: 12},
		XL:   style.Radius{Name: "xl", Value: 16},
		Full: style.Radius{Name: "full", Value: 9999},
	},
	Fonts: style.FontSet{
		Body:    style.Font{Name: "body", Value: "'Inter', system-ui, sans-serif"},
		Heading: style.Font{Name: "heading", Value: "'Inter', system-ui, sans-serif"},
		Mono:    style.Font{Name: "mono", Value: "'JetBrains Mono', monospace"},
	},
	Breakpoints: style.BreakpointSet{
		SM:  style.Breakpoint{Name: "sm", Value: 640},
		MD:  style.Breakpoint{Name: "md", Value: 768},
		LG:  style.Breakpoint{Name: "lg", Value: 1024},
		XL:  style.Breakpoint{Name: "xl", Value: 1280},
		XXL: style.Breakpoint{Name: "2xl", Value: 1536},
	},
	Shadows: style.ShadowSet{
		None: style.Shadow{Name: "none", Value: "none"},
		SM:   style.Shadow{Name: "sm", Value: "0 1px 2px 0 rgba(0,0,0,0.05)"},
		MD:   style.Shadow{Name: "md", Value: "0 4px 6px -1px rgba(0,0,0,0.10), 0 2px 4px -1px rgba(0,0,0,0.06)"},
		LG:   style.Shadow{Name: "lg", Value: "0 10px 15px -3px rgba(0,0,0,0.10), 0 4px 6px -2px rgba(0,0,0,0.05)"},
		XL:   style.Shadow{Name: "xl", Value: "0 20px 25px -5px rgba(0,0,0,0.10), 0 10px 10px -5px rgba(0,0,0,0.04)"},
	},
	ZIndex: style.ZIndexSet{
		Dropdown: style.ZIndexValue{Name: "dropdown", Value: 100},
		Sticky:   style.ZIndexValue{Name: "sticky", Value: 200},
		Modal:    style.ZIndexValue{Name: "modal", Value: 300},
		Popover:  style.ZIndexValue{Name: "popover", Value: 400},
		Toast:    style.ZIndexValue{Name: "toast", Value: 500},
	},
	Durations: style.DurationSet{
		Fast:   style.Duration{Name: "fast", Value: 150 * time.Millisecond},
		Normal: style.Duration{Name: "normal", Value: 250 * time.Millisecond},
		Slow:   style.Duration{Name: "slow", Value: 400 * time.Millisecond},
	},
	Typography: style.FontSizeSet{
		XS:   style.FontSize{Name: "xs", Value: "0.75rem"},
		SM:   style.FontSize{Name: "sm", Value: "0.875rem"},
		Base: style.FontSize{Name: "base", Value: "1rem"},
		LG:   style.FontSize{Name: "lg", Value: "1.125rem"},
		XL:   style.FontSize{Name: "xl", Value: "1.25rem"},
		XXL:  style.FontSize{Name: "2xl", Value: "1.5rem"},
		XXXL: style.FontSize{Name: "3xl", Value: "1.875rem"},
	},
}
`
