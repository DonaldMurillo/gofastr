package desktopcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/internal/appid"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
)

// RunDesktop runs `gofastr desktop <verb>` and exits non-zero when the
// verb fails or is unknown.
func RunDesktop(args []string) {
	if len(args) == 0 {
		printDesktopUsage()
		cli.OsExit(2)
		return
	}
	switch args[0] {
	case "run":
		runDesktopRun(args[1:])
	case "build":
		runDesktopBuild(args[1:])
	case "install", "uninstall":
		runDesktopInstall(args[1:], args[0] == "uninstall", runtime.GOOS)
	case "types":
		runDesktopTypes(args[1:])
	case "--help", "-h":
		printDesktopUsage()
	default:
		if desktopExtraVerb(args) {
			return
		}
		cli.Fail("Unknown desktop verb: %s (want run, build, install, uninstall, types, keygen, or feed)", args[0])
		printDesktopUsage()
		cli.OsExit(2)
	}
}

type desktopRunFlags struct {
	dir   string
	pkg   string
	watch bool
}

func parseDesktopRunFlags(args []string) (desktopRunFlags, error) {
	f := desktopRunFlags{dir: ".", pkg: "."}
	_, err := cli.ParseFlags(args,
		map[string]*string{"--dir": &f.dir, "--pkg": &f.pkg},
		map[string]*bool{"--watch": &f.watch})
	return f, err
}

func runDesktopRun(args []string) {
	if cli.HasHelpFlag(args) {
		printDesktopUsage()
		return
	}
	f, err := parseDesktopRunFlags(args)
	if err != nil {
		cli.Fail("%v", err)
		printDesktopUsage()
		cli.OsExit(2)
		return
	}

	runtimeIsolation, err := isolation.Resolve(f.dir)
	if err != nil {
		cli.Fail("Resolve isolation: %v", err)
		cli.OsExit(1)
		return
	}
	bin := devcmd.DevServerBinaryPath(runtimeIsolation)
	if bin == "" {
		cli.Fail("Could not create a private temp dir for the app binary")
		cli.OsExit(1)
		return
	}

	childEnv := devcmd.BuildDevChildEnv(runtimeIsolation.Env(os.Environ()), "")

	var mu sync.Mutex
	var child *exec.Cmd

	shutdown := make(chan struct{})
	watchCtx, endWatch := context.WithCancel(context.Background())
	defer endWatch()
	var buildInterrupt <-chan struct{}
	if f.watch {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, devcmd.ShutdownSignals...)
		go func() {
			<-signals
			close(shutdown)
			endWatch()
		}()
		buildInterrupt = shutdown
	}

	buildEnv := append(os.Environ(), "CGO_ENABLED=0")

	launch := func() bool {
		buildCmd := devcmd.BuildCommand(bin, f.pkg)
		buildCmd.Dir = f.dir
		buildCmd.Env = buildEnv
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := devcmd.RunBuildWithHeartbeat(buildCmd, f.pkg, buildInterrupt); err != nil {
			if !errors.Is(err, devcmd.ErrBuildInterrupted) {
				cli.Fail("Build failed: %v", err)
			}
			return false
		}
		if err := prepareDesktopRun(bin); err != nil {
			cli.Fail("Prepare desktop runtime: %v", err)
			return false
		}
		run := exec.Command(bin)
		run.Dir = f.dir
		run.Env = childEnv
		devcmd.TieToParent(run)
		run.Stdout = os.Stdout
		run.Stderr = os.Stderr
		mu.Lock()
		child = run
		mu.Unlock()
		if err := run.Start(); err != nil {
			cli.Fail("Failed to start: %v", err)
			return false
		}
		waitErr := make(chan error, 1)
		go func() { waitErr <- run.Wait() }()
		if !f.watch {
			err := <-waitErr
			code := 0
			if err != nil {
				code = exitCodeOf(err)
			}
			if dir := devcmd.DevServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			cli.OsExit(code)
		}
		return true
	}

	var (
		since  time.Time
		before map[string]time.Time
	)
	if f.watch {
		since = time.Now()
		before = devcmd.ScanModTimes(f.dir)
	}

	if !launch() {
		select {
		case <-shutdown:
			if dir := devcmd.DevServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			return
		default:
		}
		if dir := devcmd.DevServerBinDir(); dir != "" {
			_ = os.RemoveAll(dir)
		}
		cli.OsExit(1)
		return
	}
	if !f.watch {
		return
	}

	reload := make(chan struct{}, 1)
	stop := make(chan struct{})
	watch := devcmd.NewWatch(watchCtx, f.dir, f.pkg, buildEnv)
	go watch.Notify(before, since, stop, reload)

	for {
		select {
		case <-shutdown:
			fmt.Println()
			cli.Info("Shutting down...")
			devcmd.KillServer(&mu, &child)
			if dir := devcmd.DevServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			close(stop)
			return
		case <-reload:
			fmt.Println()
			cli.Info("Change detected; rebuilding...")
			devcmd.KillServer(&mu, &child)
			if launch() {
				cli.Success("Reloaded!")
				go watch.Refresh()
			} else {
				select {
				case <-shutdown: // the next turn shuts down
				default:
					cli.Fail("Build failed. Fixing and saving will retry")
				}
			}
		}
	}
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if ok := asExitError(err, &ee); ok {
		return ee.ExitCode()
	}
	return 1
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

type desktopBuildFlags struct {
	format        string // Linux: empty/executable preserves the plain ELF output; tar.gz is opt-in.
	formatSet     bool
	id            string
	name          string
	icon          string
	pkg           string
	version       string
	out           string
	sign          string
	noSign        bool
	timestampURL  string
	zip           bool
	scheme        string
	notarize      bool
	notaryProfile string
	launchArgs    string
	ldflags       string
	entitlements  string
}

func parseDesktopBuildFlags(args []string) (desktopBuildFlags, error) {
	f := desktopBuildFlags{pkg: ".", version: "0.1.0", out: "dist", notaryProfile: "gofastr"}
	seen, err := cli.ParseFlags(args, map[string]*string{
		"--format": &f.format, "--id": &f.id, "--name": &f.name, "--icon": &f.icon,
		"--sign": &f.sign, "--scheme": &f.scheme, "--notary-profile": &f.notaryProfile,
		"--entitlements": &f.entitlements, "--pkg": &f.pkg, "--version": &f.version,
		"-o": &f.out, "--output": &f.out, "--launch-args": &f.launchArgs, "--ldflags": &f.ldflags,
		"--timestamp-url": &f.timestampURL,
	}, map[string]*bool{"--no-sign": &f.noSign, "--notarize": &f.notarize, "--zip": &f.zip})
	f.formatSet = seen["--format"]
	return f, err
}

var desktopVersionRe = regexp.MustCompile(linuxbundle.VersionGrammar)

var desktopSchemeRe = regexp.MustCompile(linuxbundle.SchemeGrammar)

func validateDesktopID(id string) error {
	if err := appid.Check(id); err != nil {
		return fmt.Errorf("--id %w", err)
	}
	return nil
}

func validateDesktopName(name string) error {
	if err := appid.CheckName(name); err != nil {
		return fmt.Errorf("--name %w", err)
	}
	return nil
}

func runDesktopBuild(args []string) {
	runDesktopBuildForOS(args, runtime.GOOS)
}

func runDesktopBuildForOS(args []string, targetOS string) {
	if cli.HasHelpFlag(args) {
		printDesktopUsage()
		return
	}
	f, err := parseDesktopBuildFlags(args)
	if err != nil {
		cli.Fail("%v", err)
		printDesktopUsage()
		cli.OsExit(2)
		return
	}
	if f.formatSet && (targetOS != "linux" || (f.format != "executable" && f.format != "tar.gz")) {
		cli.Fail("--format supports executable or tar.gz on Linux only")
		cli.OsExit(1)
		return
	}
	if err := validateDesktopID(f.id); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	name := f.name
	if name == "" {
		seg := f.id
		if i := strings.LastIndexByte(seg, '.'); i >= 0 && i < len(seg)-1 {
			seg = seg[i+1:]
		}
		name = seg
	}
	if err := validateDesktopName(name); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if f.scheme != "" && !desktopSchemeRe.MatchString(f.scheme) {
		cli.Fail("--scheme %q must match %s", f.scheme, desktopSchemeRe.String())
		cli.OsExit(1)
		return
	}
	if _, err := extraLinkerFlags(f); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if f.noSign && f.sign != "" {
		cli.Fail("--no-sign and --sign are mutually exclusive")
		cli.OsExit(1)
		return
	}
	if err := validateNotarizeFlags(f); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if targetOS != "windows" && (f.timestampURL != "" || f.zip) {
		cli.Fail("--timestamp-url and --zip are Windows build flags")
		cli.OsExit(1)
		return
	}
	if targetOS == "windows" {
		if f.notarize || f.entitlements != "" {
			cli.Fail("Notarization is macOS-only. Windows desktop builds sign with --sign=<certificate thumbprint or subject> and --timestamp-url.")
			cli.OsExit(1)
			return
		}
		if f.timestampURL != "" && f.sign == "" {
			cli.Fail("--timestamp-url needs --sign")
			cli.OsExit(1)
			return
		}
		if f.zip {
			if err := desktop.ValidateAppVersion(f.version); err != nil || len(f.version) > 64 {
				cli.Fail("--version %q must be major.minor.patch with an optional -pre-release (no build metadata, no leading zeros), at most 64 characters, to build an update archive with --zip", f.version)
				cli.OsExit(1)
				return
			}
		}
		if err := buildWindowsDesktop(f, name); err != nil {
			cli.Fail("Windows desktop build failed: %v", err)
			cli.OsExit(1)
			return
		}
		return
	}

	if targetOS == "linux" {
		if err := buildLinuxDesktop(f, name); err != nil {
			cli.Fail("Linux desktop build failed: %v", err)
			cli.OsExit(1)
		}
		return
	}
	if targetOS != "darwin" {
		cli.Fail("Desktop build is not supported on %s", targetOS)
		cli.OsExit(1)
		return
	}
	if err := desktop.ValidateAppVersion(f.version); err != nil || len(f.version) > 64 {
		cli.Fail("--version %q must be major.minor.patch with an optional -pre-release (no build metadata, no leading zeros), at most 64 characters", f.version)
		cli.OsExit(1)
		return
	}

	archs, err := macDesktopArchs(os.Getenv("GOARCH"))
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if len(archs) > 1 {
		if _, err := lookTool("lipo"); err != nil {
			cli.Fail("A universal macOS build merges its two executables with lipo, which is not on PATH (it ships with the Xcode command line tools: xcode-select --install). Set GOARCH=arm64 or GOARCH=amd64 to build one architecture.")
			cli.OsExit(1)
			return
		}
	}

	appDir := filepath.Join(f.out, name+".app")
	if err := os.RemoveAll(appDir); err != nil {
		cli.Fail("Remove previous bundle %s: %v", appDir, err)
		cli.OsExit(1)
		return
	}
	contents := filepath.Join(appDir, "Contents")
	macOS := filepath.Join(contents, "MacOS")
	resources := filepath.Join(contents, "Resources")
	for _, d := range []string{macOS, resources} {
		//gofastr:allow(worldreadable) an .app bundle is a public artifact: Finder and LaunchServices traverse Contents/ as any user, and it holds no state or secret
		if err := os.MkdirAll(d, 0o755); err != nil {
			cli.Fail("Create %s: %v", d, err)
			cli.OsExit(1)
			return
		}
	}

	exe := filepath.Join(macOS, name)
	extra, err := extraLinkerFlags(f)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if err := buildMacExecutable(f.pkg, "-s -w"+extra, exe, archs); err != nil {
		cli.Fail("Build failed: %v", err)
		cli.OsExit(1)
		return
	}

	plist := renderInfoPlist(infoPlistValues{
		Scheme:       f.scheme,
		Name:         name,
		DisplayName:  name,
		Identifier:   f.id,
		Executable:   name,
		ShortVersion: f.version,
		Version:      f.version,
	})
	//gofastr:allow(worldreadable) bundle metadata is a public artifact: LaunchServices reads Contents/Info.plist as any user, and it holds no state or secret
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), plist, 0o644); err != nil {
		cli.Fail("Write Info.plist: %v", err)
		cli.OsExit(1)
		return
	}
	//gofastr:allow(worldreadable) bundle metadata is a public artifact: the eight-byte type/creator code Finder reads, no state and no secret
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		cli.Fail("Write PkgInfo: %v", err)
		cli.OsExit(1)
		return
	}

	iconBytes, iconSource := loadIconSource(f.icon)
	icns, err := buildICNS(iconBytes)
	if err != nil {
		cli.Fail("Build icon.icns: %v", err)
		cli.OsExit(1)
		return
	}
	//gofastr:allow(worldreadable) a bundle resource: Finder, LaunchServices, and the Dock read icon.icns as any user, it holds no state or secret
	if err := os.WriteFile(filepath.Join(resources, "icon.icns"), icns, 0o644); err != nil {
		cli.Fail("Write icon.icns: %v", err)
		cli.OsExit(1)
		return
	}

	cli.Success("Wrote %s", appDir)
	if f.notarize {
		if err := notarizeDesktopBundle(f, name, appDir); err != nil {
			cli.Fail("Notarization failed: %v", err)
			cli.OsExit(1)
			return
		}
		cli.Info("Icon source: %s.", iconSource)
		return
	}
	signDesktopBundle(f, appDir)
	cli.Info("Icon source: %s. Notarize before distributing a signed bundle.", iconSource)
}

type desktopTypesFlags struct {
	pkg string
	out string
}

func parseDesktopTypesFlags(args []string) (desktopTypesFlags, error) {
	f := desktopTypesFlags{pkg: ".", out: "desktop.d.ts"}
	_, err := cli.ParseFlags(args, map[string]*string{"--pkg": &f.pkg, "--out": &f.out}, nil)
	return f, err
}

func runDesktopTypes(args []string) {
	if cli.HasHelpFlag(args) {
		printDesktopUsage()
		return
	}
	f, err := parseDesktopTypesFlags(args)
	if err != nil {
		cli.Fail("%v", err)
		printDesktopUsage()
		cli.OsExit(2)
		return
	}

	if err := writeDesktopTypes(f); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
}

func writeDesktopTypes(f desktopTypesFlags) error {
	dir, err := os.MkdirTemp("", "gofastr-desktop-types-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "app")
	if goos := runtime.GOOS; goos == "windows" {
		bin += ".exe"
	}

	cli.Info("Building %s...", f.pkg)
	buildCmd := exec.Command("go", "build", "-o", bin, f.pkg)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("Build failed: %w", err)
	}

	cli.Info("Running the app for its manifest...")
	run := exec.Command(bin)
	run.Env = append(os.Environ(), desktop.ManifestEnv+"=1")
	var stdout bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("App run failed: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.UseNumber()
	var manifest desktop.Manifest
	if err := dec.Decode(&manifest); err != nil {
		return fmt.Errorf("Manifest output is not JSON: %w", err)
	}

	dts := desktop.DTS(manifest)
	//gofastr:allow(worldreadable) generated source the developer commits (a .d.ts), same posture as generate sdk output
	if err := os.WriteFile(f.out, []byte(dts), 0o644); err != nil {
		return fmt.Errorf("Write %s: %w", f.out, err)
	}
	cli.Success("Wrote %s (%d capabilities)", f.out, len(manifest.Capabilities))
	return nil
}
