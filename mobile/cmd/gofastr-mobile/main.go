// Command gofastr-mobile builds a GoFastr app into a phone app and runs it
// on the iOS simulator or an Android emulator, from the command line: no
// Xcode project, no Gradle, no IDE. PROTOTYPE.
//
//	gofastr-mobile doctor [--platform ios-sim|android-emu]
//	gofastr-mobile build  --platform ios-sim|android-emu --pkg ./examples/notes/notesmobile
//	gofastr-mobile run    --platform ios-sim|android-emu --pkg ./examples/notes/notesmobile [--screenshot shot.png]
//
// Run it from the Go module that holds the bind package (the module must
// require golang.org/x/mobile; this one pins it with a tool directive).
// --dry-run prints every step without running any.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

const usage = `gofastr-mobile builds a GoFastr app into a phone app and runs it.

Usage:
  gofastr-mobile doctor [--platform ios-sim|android-emu]
  gofastr-mobile build  --platform ios-sim|android-emu --pkg <bind package> [flags]
  gofastr-mobile run    --platform ios-sim|android-emu --pkg <bind package> [flags]

Flags (build, run):
  --platform    ios-sim (iOS simulator, needs a Mac with Xcode installed)
                android-emu (Android emulator or attached device, needs the
                Android SDK, NDK and a JDK)
  --pkg         the bind package: exports Start, Stop, Handle, Open
                (see examples/notes/notesmobile)
  --id          bundle / application id (default dev.gofastr.<package>)
  --name        name on the home screen (default: the package name)
  --version     app version (default 0.1.0)
  --out         output directory (default dist/mobile/<platform>)
  --dry-run     print every step without running it

Flags (run only):
  --device      simulator name or UDID / adb serial or AVD name
  --screenshot  write a screenshot here once the app is up
  --wait        how long to wait before the screenshot (default 8s)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	platform := fs.String("platform", "", "ios-sim or android-emu")
	pkg := fs.String("pkg", "", "bind package")
	id := fs.String("id", "", "bundle / application id")
	name := fs.String("name", "", "home-screen name")
	version := fs.String("version", "", "app version")
	out := fs.String("out", "", "output directory")
	dry := fs.Bool("dry-run", false, "print the steps only")
	device := fs.String("device", "", "simulator / device / AVD")
	shot := fs.String("screenshot", "", "screenshot path")
	wait := fs.Duration("wait", 8*time.Second, "wait before the screenshot")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	r := &runner{ctx: ctx, out: stdout, dryRun: *dry}

	switch sub {
	case "doctor":
		return doctor(r, *platform)
	case "build", "run":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", sub, usage)
		return 2
	}
	if *platform != "ios-sim" && *platform != "android-emu" {
		fmt.Fprintln(stderr, "--platform must be ios-sim or android-emu")
		return 2
	}
	if *pkg == "" {
		fmt.Fprintln(stderr, "--pkg is required (for this module: --pkg ./examples/notes/notesmobile)")
		return 2
	}
	if *wait < 0 {
		fmt.Fprintln(stderr, "--wait must not be negative")
		return 2
	}
	pkgPath, pkgName, err := resolvePkg(r, *pkg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	a, err := newApp(pkgPath, pkgName, *id, *name, *version)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join("dist", "mobile", *platform)
	}
	if dir, err = filepath.Abs(dir); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *shot != "" {
		if *shot, err = filepath.Abs(*shot); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if !r.dryRun {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "gofastr-mobile %s: %s (%s) from %s\n", sub, a.Name, a.ID, a.PkgPath)
	opts := runOpts{device: *device, screenshot: *shot, wait: *wait}

	var artifact string
	switch *platform {
	case "ios-sim":
		if artifact, err = buildIOS(r, a, dir); err == nil && sub == "run" {
			err = runIOS(r, a.forIOS(), artifact, opts)
		}
	case "android-emu":
		if artifact, err = buildAndroid(r, a, dir); err == nil && sub == "run" {
			err = runAndroid(r, a, artifact, opts)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "\ngofastr-mobile: %v\n", err)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	if r.dryRun {
		fmt.Fprintln(stdout, "\n(dry run: nothing was executed)")
	} else {
		fmt.Fprintf(stdout, "\nBuilt %s\n", artifact)
	}
	return 0
}
