package main

// The `gofastr desktop` subcommand: run (build + launch with dev tools),
// build (a .app bundle), and types (a .d.ts from the app's real
// manifest). The posture is the blueprint's: the CLI never owns app
// code, it compiles the app you wrote and wraps the output.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func runDesktop(args []string) {
	if len(args) == 0 {
		printDesktopUsage()
		osExit(1)
		return
	}
	switch args[0] {
	case "run":
		runDesktopRun(args[1:])
	case "build":
		runDesktopBuild(args[1:])
	case "types":
		runDesktopTypes(args[1:])
	case "--help", "-h":
		printDesktopUsage()
	default:
		if desktopExtraVerb(args) {
			return
		}
		fail("Unknown desktop verb: %s (want run, build, types, keygen, or feed)", args[0])
		printDesktopUsage()
		osExit(1)
	}
}

// ── gofastr desktop run ───────────────────────────────────────────────

type desktopRunFlags struct {
	dir   string
	pkg   string
	watch bool
}

func parseDesktopRunFlags(args []string) desktopRunFlags {
	f := desktopRunFlags{dir: ".", pkg: "."}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := splitFlagEqual(args[i])
		switch name {
		case "--dir":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.dir = v
			}
		case "--pkg":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.pkg = v
			}
		case "--watch":
			f.watch = true
		}
	}
	return f
}

func runDesktopRun(args []string) {
	f := parseDesktopRunFlags(args)

	runtimeIsolation, err := isolation.Resolve(f.dir)
	if err != nil {
		fail("Resolve isolation: %v", err)
		osExit(1)
		return
	}
	bin := devServerBinaryPath(runtimeIsolation)
	if bin == "" {
		fail("Could not create a private temp dir for the app binary")
		osExit(1)
		return
	}

	// No --addr: the desktop host picks its own loopback port inside
	// Run. GOFASTR_DEV=1 mounts the dev MCP and livereload injector in
	// the window, exactly as `gofastr dev` does for the browser.
	childEnv := buildDevChildEnv(runtimeIsolation.Env(os.Environ()), "")

	var mu sync.Mutex
	var child *exec.Cmd

	launch := func() bool {
		buildCmd := exec.Command("go", "build", "-o", bin, f.pkg)
		buildCmd.Dir = f.dir
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			fail("Build failed: %v", err)
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
			fail("Failed to start: %v", err)
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
			if dir := devServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			osExit(code)
		}
		return true
	}

	if !launch() {
		osExit(1)
		return
	}
	if !f.watch {
		return
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	reload := make(chan struct{}, 1)
	stop := make(chan struct{})
	go func() {
		prev := scanModTimes(f.dir)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				curr := scanModTimes(f.dir)
				if changed(prev, curr) {
					prev = curr
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
			info("Shutting down...")
			killServer(&mu, &child)
			if dir := devServerBinDir(); dir != "" {
				_ = os.RemoveAll(dir)
			}
			close(stop)
			return
		case <-reload:
			fmt.Println()
			info("Change detected; rebuilding...")
			killServer(&mu, &child)
			if launch() {
				success("Reloaded!")
			} else {
				fail("Build failed. Fixing and saving will retry")
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
	id      string
	name    string
	icon    string
	pkg     string
	version string
	out     string
	// sign is a codesign identity ("" with noSign false means the
	// default: ad-hoc when codesign exists).
	sign   string
	noSign bool
	// scheme is the custom URL scheme the bundle claims
	// (CFBundleURLTypes); empty claims nothing.
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
	entitlements  string
}

func parseDesktopBuildFlags(args []string) desktopBuildFlags {
	f := desktopBuildFlags{pkg: ".", version: "0.1.0", out: "dist", notaryProfile: "gofastr"}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := splitFlagEqual(args[i])
		switch name {
		case "--id":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.id = v
			}
		case "--name":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.name = v
			}
		case "--icon":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.icon = v
			}
		case "--sign":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.sign = v
			}
		case "--no-sign":
			f.noSign = true
		case "--scheme":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.scheme = v
			}
		case "--notarize":
			f.notarize = true
		case "--notary-profile":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.notaryProfile = v
			}
		case "--entitlements":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.entitlements = v
			}
		case "--pkg":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.pkg = v
			}
		case "--version":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.version = v
			}
		case "-o", "--output":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.out = v
			}
		}
	}
	return f
}

var desktopIDRe = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
var desktopVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9A-Za-z]+)*$`)

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

func runDesktopBuild(args []string) {
	f := parseDesktopBuildFlags(args)
	if err := validateDesktopID(f.id); err != nil {
		fail("%v", err)
		osExit(1)
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
		fail("%v", err)
		osExit(1)
		return
	}
	if f.noSign && f.sign != "" {
		fail("--no-sign and --sign are mutually exclusive")
		osExit(1)
		return
	}
	if err := validateNotarizeFlags(f); err != nil {
		fail("%v", err)
		osExit(1)
		return
	}

	appDir := filepath.Join(f.out, name+".app")
	contents := filepath.Join(appDir, "Contents")
	macOS := filepath.Join(contents, "MacOS")
	resources := filepath.Join(contents, "Resources")
	for _, d := range []string{macOS, resources} {
		//gofastr:allow(worldreadable) an .app bundle is a public artifact: Finder and LaunchServices traverse Contents/ as any user, and it holds no state or secret
		if err := os.MkdirAll(d, 0o755); err != nil {
			fail("Create %s: %v", d, err)
			osExit(1)
			return
		}
	}

	exe := filepath.Join(macOS, name)
	info("Building %s (darwin/arm64, CGO_ENABLED=0)...", f.pkg)
	buildCmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", exe, f.pkg)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fail("Build failed: %v", err)
		osExit(1)
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
		fail("Write Info.plist: %v", err)
		osExit(1)
		return
	}
	//gofastr:allow(worldreadable) bundle metadata is a public artifact: the eight-byte type/creator code Finder reads, no state and no secret
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		fail("Write PkgInfo: %v", err)
		osExit(1)
		return
	}

	iconBytes, iconSource := loadIconSource(f.icon)
	icns, err := buildICNS(iconBytes)
	if err != nil {
		fail("Build icon.icns: %v", err)
		osExit(1)
		return
	}
	//gofastr:allow(worldreadable) a bundle resource: Finder, LaunchServices, and the Dock read icon.icns as any user, it holds no state or secret
	if err := os.WriteFile(filepath.Join(resources, "icon.icns"), icns, 0o644); err != nil {
		fail("Write icon.icns: %v", err)
		osExit(1)
		return
	}

	success("Wrote %s", appDir)
	if f.notarize {
		// A signing or notarization failure is fatal here, unlike the
		// best-effort ad-hoc default: a half-notarized bundle must not
		// look like a shippable one.
		if err := notarizeDesktopBundle(f, name, appDir); err != nil {
			fail("Notarization failed: %v", err)
			osExit(1)
			return
		}
		info("Icon source: %s.", iconSource)
		return
	}
	signDesktopBundle(f, appDir)
	info("Icon source: %s. Notarize before distributing a signed bundle.", iconSource)
}

// signDesktopBundle signs the finished bundle per the build flags.
// --no-sign skips; --sign=<identity> uses that identity; the default
// signs ad-hoc when codesign is on PATH. A signing failure is printed,
// never fatal: an unsigned bundle still runs (only notifications need
// a signature).
func signDesktopBundle(f desktopBuildFlags, appDir string) {
	if f.noSign {
		info("The bundle is unsigned (--no-sign); macOS notifications will not work.")
		return
	}
	identity := f.sign
	if identity == "" {
		if _, err := exec.LookPath("codesign"); err != nil {
			info("The bundle is unsigned: no codesign on this host; macOS notifications will not work.")
			return
		}
		identity = "-" // ad-hoc
	}
	out, err := runTool("codesign", "--force", "--deep", "--sign", identity, appDir)
	if err != nil {
		fail("Signing failed; the bundle stays unsigned: %v", toolDetail(out, err))
		return
	}
	if f.sign == "" {
		success("Signed %s ad-hoc (codesign --sign -); notifications work after the first-run Allow.", appDir)
	} else {
		success("Signed %s with identity %q.", appDir, f.sign)
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
			fail("Read --icon: %v", err)
			osExit(1)
		}
		return b, path
	}
	img, err := fwimage.NewGradient(1024, 1024, "#0E7C86", "#0E7C86")
	if err != nil {
		fail("Generate default icon: %v", err)
		osExit(1)
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		fail("Encode default icon: %v", err)
		osExit(1)
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

// squareCropStd center-crops img to a square via the stdlib (NRGBA
// SubImage when available, a draw otherwise), so a non-square --icon
// does not stretch.
func squareCropStd(img stdimage.Image) stdimage.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := min(w, h)
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

func parseDesktopTypesFlags(args []string) desktopTypesFlags {
	f := desktopTypesFlags{pkg: ".", out: "desktop.d.ts"}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := splitFlagEqual(args[i])
		switch name {
		case "--pkg":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.pkg = v
			}
		case "--out":
			if v, ok := flagValue(value, hasValue, args, &i); ok {
				f.out = v
			}
		}
	}
	return f
}

func runDesktopTypes(args []string) {
	f := parseDesktopTypesFlags(args)

	dir, err := os.MkdirTemp("", "gofastr-desktop-types-*")
	if err != nil {
		fail("Temp dir: %v", err)
		osExit(1)
		return
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "app")
	if goos := runtime.GOOS; goos == "windows" {
		bin += ".exe"
	}

	info("Building %s...", f.pkg)
	buildCmd := exec.Command("go", "build", "-o", bin, f.pkg)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fail("Build failed: %v", err)
		osExit(1)
		return
	}

	// One headless run of the real app: battery/desktop's Run freezes
	// the registry, prints the manifest JSON, and exits before touching
	// the shell or the listener.
	info("Running the app for its manifest...")
	run := exec.Command(bin)
	run.Env = append(os.Environ(), desktop.ManifestEnv+"=1")
	var stdout bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		fail("App run failed: %v", err)
		osExit(1)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.UseNumber()
	var manifest desktop.Manifest
	if err := dec.Decode(&manifest); err != nil {
		fail("Manifest output is not JSON: %v", err)
		osExit(1)
		return
	}

	dts := desktop.DTS(manifest)
	//gofastr:allow(worldreadable) generated source the developer commits (a .d.ts), same posture as generate sdk output
	if err := os.WriteFile(f.out, []byte(dts), 0o644); err != nil {
		fail("Write %s: %v", f.out, err)
		osExit(1)
		return
	}
	success("Wrote %s (%d capabilities)", f.out, len(manifest.Capabilities))
}
