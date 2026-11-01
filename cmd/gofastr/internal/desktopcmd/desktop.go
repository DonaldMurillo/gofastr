package desktopcmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"image/draw"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func RunDesktop(args []string) {
	if len(args) == 0 {
		printDesktopUsage()
		cli.OsExit(1)
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
		cli.OsExit(1)
	}
}

// ── gofastr desktop run ───────────────────────────────────────────────

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
		cli.OsExit(1)
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

	// No --addr: the desktop host picks its own loopback port inside
	// Run. GOFASTR_DEV=1 mounts the dev MCP and livereload injector in
	// the window, exactly as `gofastr dev` does for the browser.
	childEnv := devcmd.BuildDevChildEnv(runtimeIsolation.Env(os.Environ()), "")

	var mu sync.Mutex
	var child *exec.Cmd

	// --watch owns SIGINT/SIGTERM from the start, so a signal during a
	// build ends the build (devcmd.RunBuildWithHeartbeat) instead of
	// waiting for it and the app it starts. shutdown closes on the first.
	// Without --watch nothing owns the signal: the build gets a nil
	// interrupt and stays in the terminal's process group, where Ctrl-C
	// reaches it directly.
	shutdown := make(chan struct{})
	// watchCtx ends with the signal: the watcher's go list can stall on
	// a module proxy, and shutdown does not wait for it.
	watchCtx, endWatch := context.WithCancel(context.Background())
	defer endWatch()
	var buildInterrupt <-chan struct{}
	if f.watch {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-signals
			close(shutdown)
			endWatch()
		}()
		buildInterrupt = shutdown
	}

	// buildEnv is the build's environment, and the watcher's go list
	// runs with the same one: CGO_ENABLED picks files, and files pick
	// the imports the watch keeps.
	buildEnv := append(os.Environ(), "CGO_ENABLED=0")

	launch := func() bool {
		buildCmd := exec.Command("go", "build", "-o", bin, f.pkg)
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
			// Run once: block until the window closes, then exit with
			// the child's status.
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

	// Scanned before the first build: FirstSnapshot keeps these times,
	// so a save made during the build or the graph read rebuilds.
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
			// Stopped during the first build: nothing ran, nothing failed.
			if dir := devcmd.DevServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			return
		default:
		}
		cli.OsExit(1)
		return
	}
	if !f.watch {
		return
	}

	reload := make(chan struct{}, 1)
	stop := make(chan struct{})
	// A signal after the build but before here leaves shutdown closed:
	// the loop below sees it on its first turn, and watchCtx has already
	// ended the dependency query.
	watch := devcmd.NewWatch(watchCtx, f.dir, f.pkg, buildEnv)
	go func() {
		poll := watch.Poller(before, since)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if poll.Changed() {
					select {
					case reload <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

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

// exitCodeOf digs the exit status out of a Wait error without claiming
// every failure is a crash.
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

// ── gofastr desktop build ─────────────────────────────────────────────

type desktopBuildFlags struct {
	format    string // Linux: empty/executable preserves the plain ELF output; tar.gz is opt-in.
	formatSet bool
	id        string
	name      string
	icon      string
	pkg       string
	version   string
	out       string
	// sign is a codesign identity ("" with noSign false means the
	// default: ad-hoc when codesign exists).
	sign   string
	noSign bool
	// scheme is the build's custom URL scheme. macOS writes CFBundleURLTypes;
	// Windows embeds it for per-user registration at startup. Linux embeds
	// argv/session-bus handoff only, without installing an OS association.
	scheme string
	// notarize signs with --sign's identity under the hardened
	// runtime, submits to Apple's notary service, staples, re-zips;
	// any step failing fails the build. notaryProfile names the
	// keychain profile from `xcrun notarytool store-credentials`
	// (default "gofastr"); entitlements is a plist path (default: a
	// generated empty-dict plist, the correct baseline for a Go
	// binary hosting a WKWebView).
	notarize      bool
	notaryProfile string
	// launchArgs is --launch-args: arguments a double-clicked
	// executable starts with (desktop.LaunchArgs), for a binary whose
	// desktop mode is a subcommand. ldflags is --ldflags, appended to
	// the build's own linker flags (-X main.version=...).
	launchArgs   string
	ldflags      string
	entitlements string
}

func parseDesktopBuildFlags(args []string) (desktopBuildFlags, error) {
	f := desktopBuildFlags{pkg: ".", version: "0.1.0", out: "dist", notaryProfile: "gofastr"}
	seen, err := cli.ParseFlags(args, map[string]*string{
		"--format": &f.format, "--id": &f.id, "--name": &f.name, "--icon": &f.icon,
		"--sign": &f.sign, "--scheme": &f.scheme, "--notary-profile": &f.notaryProfile,
		"--entitlements": &f.entitlements, "--pkg": &f.pkg, "--version": &f.version,
		"-o": &f.out, "--output": &f.out, "--launch-args": &f.launchArgs, "--ldflags": &f.ldflags,
	}, map[string]*bool{"--no-sign": &f.noSign, "--notarize": &f.notarize})
	f.formatSet = seen["--format"]
	return f, err
}

var desktopIDRe = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

var desktopVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9A-Za-z]+)*$`)

var desktopSchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]{0,31}$`)

// validateDesktopID enforces the reverse-DNS grammar (same as
// battery/desktop's Config.ID validation).
func validateDesktopID(id string) error {
	if !desktopIDRe.MatchString(id) || !strings.Contains(id, ".") {
		return fmt.Errorf("--id %q must be reverse-DNS: [A-Za-z0-9.-]+ with at least one dot", id)
	}
	// Mirrors battery/desktop's validateAppID: the id becomes both the
	// bundle's CFBundleIdentifier and a path segment under the OS user
	// config dir, and ".", "..", "..." all satisfy the grammar AND the
	// "contains a dot" rule. A bundle built with --id .. would write
	// app.db, the session secret and the local owner id one level ABOVE
	// its data dir on every launch.
	if strings.Trim(id, ".-") == "" {
		return fmt.Errorf("--id %q must not be a bare dot/dash sequence: "+
			"it names the bundle identifier and a directory under the OS user config dir", id)
	}
	if len(id) > 253 {
		return fmt.Errorf("--id is longer than 253 characters")
	}
	return nil
}

// validateDesktopName: printable, no path separator, bounded.
func validateDesktopName(name string) error {
	if name == "" {
		return fmt.Errorf("--name must not be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("--name is longer than 64 characters")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("--name must not contain a path separator")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("--name must be printable (control character at %q)", r)
		}
	}
	return nil
}

func validWindowsArtifactName(name string) bool {
	if name == "" || strings.ContainsAny(name, `<>:"/\\|?*`) || strings.TrimRight(name, ". ") != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = strings.TrimRight(base[:i], ". ")
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
		"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
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
		cli.OsExit(1)
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
	if targetOS == "windows" {
		if f.sign != "" || f.noSign || f.notarize {
			cli.Fail("Windows desktop builds support --id, --name, --pkg, --version, --icon, --scheme, --launch-args, --ldflags, and -o. Signing and notarization are macOS-only.")
			cli.OsExit(1)
			return
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
	// Info.plist's CFBundleShortVersionString is the running version the
	// updater compares as semver: a bundle built with "1.0" would fail
	// every update check, so refuse it before building.
	if err := desktop.ValidateAppVersion(f.version); err != nil || len(f.version) > 64 {
		cli.Fail("--version %q must be major.minor.patch with an optional -pre-release (no build metadata, no leading zeros), at most 64 characters", f.version)
		cli.OsExit(1)
		return
	}

	appDir := filepath.Join(f.out, name+".app")
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

	archWarn, err := macDesktopArch(os.Getenv("GOARCH"), runtime.GOARCH)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if archWarn != "" {
		cli.Warn("%s", archWarn)
	}

	exe := filepath.Join(macOS, name)
	cli.Info("Building %s (darwin/arm64, CGO_ENABLED=0)...", f.pkg)
	extra, err := extraLinkerFlags(f)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	buildCmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w"+extra, "-o", exe, f.pkg)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
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
		// A signing or notarization failure is fatal here, unlike the
		// best-effort ad-hoc default: a half-notarized bundle must not
		// look like a shippable one.
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

// signDesktopBundle signs the finished bundle per the build flags.
// --no-sign skips; --sign=<identity> uses that identity; the default
// signs ad-hoc when codesign is on PATH. A signing failure is printed,
// never fatal: an unsigned bundle still runs (only notifications need
// a signature).
func signDesktopBundle(f desktopBuildFlags, appDir string) {
	if f.noSign {
		cli.Info("The bundle is unsigned (--no-sign); macOS notifications will not work.")
		return
	}
	identity := f.sign
	if identity == "" {
		if _, err := exec.LookPath("codesign"); err != nil {
			cli.Info("The bundle is unsigned: no codesign on this host; macOS notifications will not work.")
			return
		}
		identity = "-" // ad-hoc
	}
	out, err := runTool("codesign", "--force", "--deep", "--sign", identity, appDir)
	if err != nil {
		cli.Fail("Signing failed; the bundle stays unsigned: %v", toolDetail(out, err))
		return
	}
	if f.sign == "" {
		cli.Success("Signed %s ad-hoc (codesign --sign -); notifications work after the first-run Allow.", appDir)
	} else {
		cli.Success("Signed %s with identity %q.", appDir, f.sign)
	}
}

// loadIconSource returns the PNG bytes for the bundle icon: --icon's
// file, else a generated flat square (uihost.WithAppIcon has no default
// icon of its own, so the CLI generates one, the no-binary-assets
// posture).
func loadIconSource(path string) ([]byte, string) {
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			cli.Fail("Read --icon: %v", err)
			cli.OsExit(1)
		}
		return b, path
	}
	img, err := fwimage.NewGradient(1024, 1024, "#0E7C86", "#0E7C86")
	if err != nil {
		cli.Fail("Generate default icon: %v", err)
		cli.OsExit(1)
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		cli.Fail("Encode default icon: %v", err)
		cli.OsExit(1)
	}
	return b, "generated flat square (framework/image)"
}

// ── ICNS (pure Go, no iconutil) ───────────────────────────────────────

// icnsEntry is one representation in the container: a 4-byte type code
// and a PNG payload at the square size.
type icnsEntry struct {
	Type string
	Size int
}

// icnsEntries per the brief: the sizes macOS reads from an .icns. The
// @2x types (ic11..ic14) still carry their pixel payload sizes.
var icnsEntries = []icnsEntry{
	{"ic07", 128},
	{"ic08", 256},
	{"ic09", 512},
	{"ic10", 1024},
	{"ic11", 32},
	{"ic12", 64},
	{"ic13", 256},
	{"ic14", 512},
}

// buildICNS assembles the ICNS container: big-endian "icns" header with
// the total length, then entries of type(4) + length(4, header
// included) + PNG payload. The source PNG is center-cropped to a square
// and resized per entry.
func buildICNS(png []byte) ([]byte, error) {
	src, err := fwimage.DecodeBytes(png)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	square := squareCropStd(src.GoImage())

	var entries bytes.Buffer
	for _, e := range icnsEntries {
		resized := fwimage.FromImage(square, fwimage.FormatPNG).Resize(e.Size, e.Size)
		payload, err := resized.PNG().Bytes()
		if err != nil {
			return nil, fmt.Errorf("encode %s (%dpx): %w", e.Type, e.Size, err)
		}
		entries.WriteString(e.Type)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)+8))
		entries.Write(lenBuf[:])
		entries.Write(payload)
	}

	body := entries.Bytes()
	out := bytes.NewBuffer(make([]byte, 0, len(body)+8))
	out.WriteString("icns")
	var total [4]byte
	binary.BigEndian.PutUint32(total[:], uint32(len(body)+8))
	out.Write(total[:])
	out.Write(body)
	return out.Bytes(), nil
}

// buildWindowsICO converts the same PNG icon source used by macOS into
// a multi-resolution ICO container. Its frames carry PNG payloads, which
// Windows supports for desktop icons on current versions.
func buildWindowsICO(pngBytes []byte) ([]byte, error) {
	src, err := fwimage.DecodeBytes(pngBytes)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	square := squareCropStd(src.GoImage())
	const count = 4
	const headerBytes = 6 + count*16
	out := make([]byte, headerBytes)
	binary.LittleEndian.PutUint16(out[2:4], 1) // ICONDIR type
	binary.LittleEndian.PutUint16(out[4:6], count)
	offset := uint32(headerBytes)
	for i, size := range []int{16, 32, 48, 256} {
		resized := fwimage.FromImage(square, fwimage.FormatPNG).Resize(size, size)
		payload, err := resized.PNG().Bytes()
		if err != nil {
			return nil, fmt.Errorf("encode Windows icon (%dpx): %w", size, err)
		}
		entry := 6 + i*16
		if size < 256 {
			out[entry], out[entry+1] = byte(size), byte(size)
		}
		binary.LittleEndian.PutUint16(out[entry+4:entry+6], 1)  // planes
		binary.LittleEndian.PutUint16(out[entry+6:entry+8], 32) // bits per pixel
		binary.LittleEndian.PutUint32(out[entry+8:entry+12], uint32(len(payload)))
		binary.LittleEndian.PutUint32(out[entry+12:entry+16], offset)
		out = append(out, payload...)
		offset += uint32(len(payload))
	}
	return out, nil
}

// squareCropStd center-crops img to a square via the stdlib (NRGBA
// SubImage when available, a draw otherwise), so a non-square --icon
// does not stretch.
func squareCropStd(img stdimage.Image) stdimage.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := cli.Min(w, h)
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	rect := stdimage.Rect(x0, y0, x0+side, y0+side)
	if sub, ok := img.(interface {
		SubImage(r stdimage.Rectangle) stdimage.Image
	}); ok {
		return sub.SubImage(rect)
	}
	dst := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	// Return a fresh opaque square so the caller's bounds are stable.
	filled := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(filled, filled.Bounds(), dst, stdimage.Point{}, draw.Src)
	return filled
}

// ── gofastr desktop types ─────────────────────────────────────────────

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
		cli.OsExit(1)
		return
	}

	dir, err := os.MkdirTemp("", "gofastr-desktop-types-*")
	if err != nil {
		cli.Fail("Temp dir: %v", err)
		cli.OsExit(1)
		return
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
		cli.Fail("Build failed: %v", err)
		cli.OsExit(1)
		return
	}

	// One headless run of the real app: battery/desktop's Run freezes
	// the registry, prints the manifest JSON, and exits before touching
	// the shell or the listener.
	cli.Info("Running the app for its manifest...")
	run := exec.Command(bin)
	run.Env = append(os.Environ(), desktop.ManifestEnv+"=1")
	var stdout bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		cli.Fail("App run failed: %v", err)
		cli.OsExit(1)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.UseNumber()
	var manifest desktop.Manifest
	if err := dec.Decode(&manifest); err != nil {
		cli.Fail("Manifest output is not JSON: %v", err)
		cli.OsExit(1)
		return
	}

	dts := desktop.DTS(manifest)
	//gofastr:allow(worldreadable) generated source the developer commits (a .d.ts), same posture as generate sdk output
	if err := os.WriteFile(f.out, []byte(dts), 0o644); err != nil {
		cli.Fail("Write %s: %v", f.out, err)
		cli.OsExit(1)
		return
	}
	cli.Success("Wrote %s (%d capabilities)", f.out, len(manifest.Capabilities))
}

// macDesktopArch checks the macOS bundle's architecture. The shell
// (battery/desktop/macos) is arm64-only, so the bundle is always
// darwin/arm64: a GOARCH naming anything else is refused, and building
// on an Intel host returns a warning that the bundle will not launch
// there. Cross-building for Apple silicon from an Intel Mac stays legal.
func macDesktopArch(envArch, hostArch string) (warn string, err error) {
	if envArch != "" && envArch != "arm64" {
		return "", fmt.Errorf("macOS desktop builds target arm64 only (Apple silicon), not GOARCH=%s", envArch)
	}
	if hostArch != "arm64" {
		return fmt.Sprintf("The bundle targets Apple silicon (arm64) and will not launch on this %s Mac.", hostArch), nil
	}
	return "", nil
}
