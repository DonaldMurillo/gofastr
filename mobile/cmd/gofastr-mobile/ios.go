package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The iOS simulator build never touches an Xcode project. Like
// `gofastr desktop build`, it assembles the .app folder itself:
//
//	gomobile bind   -> <Prefix>.xcframework (the Go app as a static framework)
//	swiftc          -> the host executable (App.swift, linked to the framework)
//	Info.plist      -> written from a template
//	codesign -s -   -> ad-hoc signature (all the simulator asks for)
//	simctl          -> boot, install, launch, screenshot
//
// Xcode must be installed (the iOS SDK and swiftc ship only inside it), but
// nothing here opens it. A physical device additionally needs a
// development team and provisioning profile; that is not wired yet.

func buildIOS(r *runner, a app, out string) (string, error) {
	a = a.forIOS()
	work := filepath.Join(out, "work")
	bin := filepath.Join(work, "bin")
	if err := bootstrapGomobile(r, bin); err != nil {
		return "", err
	}
	sdk, err := r.output("find the iOS simulator SDK", "<iphonesimulator-sdk>", "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
	if err != nil {
		return "", fmt.Errorf("%w\n    (is Xcode installed and selected? run: gofastr-mobile doctor --platform ios-sim)", err)
	}
	xcf := filepath.Join(work, a.Prefix+".xcframework")
	if err := r.run("bind the Go app as an iOS simulator framework",
		filepath.Join(bin, "gomobile"), "bind", "-target=iossimulator", "-iosversion="+a.MinOS, "-o", xcf, a.PkgPath); err != nil {
		return "", err
	}
	slice := filepath.Join(xcf, "<simulator-slice>")
	if !r.dryRun {
		if slice, err = simulatorSlice(xcf, a.Prefix); err != nil {
			return "", err
		}
	}
	src := filepath.Join(work, "ios", "App.swift")
	plist := filepath.Join(work, "ios", "Info.plist")
	if err := r.do("generate the Swift host and Info.plist", func() error {
		if err := writeRendered("templates/ios/App.swift.tmpl", src, a); err != nil {
			return err
		}
		return writeRendered("templates/ios/Info.plist.tmpl", plist, a)
	}); err != nil {
		return "", err
	}
	appDir := filepath.Join(out, a.Exe+".app")
	if err := r.do("create "+appDir, func() error {
		if err := os.RemoveAll(appDir); err != nil {
			return err
		}
		return os.MkdirAll(appDir, 0o700)
	}); err != nil {
		return "", err
	}
	target := simArch() + "-apple-ios" + a.MinOS + "-simulator"
	if err := r.run("compile the Swift host and link the Go framework",
		"xcrun", "--sdk", "iphonesimulator", "swiftc",
		"-parse-as-library", "-O",
		"-target", target, "-sdk", sdk,
		"-F", slice, "-framework", a.Prefix,
		// The Go runtime inside the framework resolves DNS and verifies
		// certificates through these; a static framework does not carry
		// its link flags, so the final link names them.
		"-framework", "Foundation", "-framework", "Security", "-framework", "CoreFoundation", "-lresolv",
		"-o", filepath.Join(appDir, a.Exe), src); err != nil {
		return "", err
	}
	if err := r.do("copy Info.plist into the bundle", func() error {
		b, err := os.ReadFile(plist)
		if err != nil {
			return err
		}
		return writeOwnerOnly(filepath.Join(appDir, "Info.plist"), b)
	}); err != nil {
		return "", err
	}
	if err := r.run("sign the bundle ad hoc", "codesign", "--force", "--sign", "-", "--timestamp=none", appDir); err != nil {
		return "", err
	}
	return appDir, nil
}

func runIOS(r *runner, a app, appDir string, opts runOpts) error {
	listing, err := r.outputBytes("list the available simulators", []byte(`{"devices":{}}`), "xcrun", "simctl", "list", "devices", "available", "--json")
	if err != nil {
		return err
	}
	udid := "<simulator-udid>"
	if !r.dryRun {
		if udid, err = pickSimulator(listing, opts.device); err != nil {
			return err
		}
	}
	r.tolerate("boot the simulator (an already-booted one is fine)", "xcrun", "simctl", "boot", udid)
	r.tolerate("show the Simulator window", "open", "-a", "Simulator")
	if err := r.run("wait until the simulator has booted", "xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
		return err
	}
	if err := r.run("install the app", "xcrun", "simctl", "install", udid, appDir); err != nil {
		return err
	}
	if err := r.run("launch the app", "xcrun", "simctl", "launch", "--terminate-running-process", udid, a.ID); err != nil {
		return err
	}
	if opts.screenshot != "" {
		r.sleep(opts.wait, "for the first page")
		if err := r.run("take a screenshot", "xcrun", "simctl", "io", udid, "screenshot", opts.screenshot); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "\nRunning on simulator %s.\n  Logs:      xcrun simctl spawn %s log stream --predicate 'process == \"%s\"'\n  Inspector: Safari > Develop > Simulator > %s\n", udid, udid, a.Exe, a.Name)
	return nil
}

// simArch is the simulator architecture of this Mac.
func simArch() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return "arm64"
}

// simulatorSlice finds the framework directory gomobile wrote for the
// simulator inside the xcframework (its name varies with the arches).
func simulatorSlice(xcf, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(xcf, "*simulator*", prefix+".framework"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no simulator slice in %s (gomobile bind -target=iossimulator should have written one)", xcf)
	}
	return filepath.Dir(matches[0]), nil
}

type simDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
	runtime     []int
}

// pickSimulator chooses the simulator: the one --device names (UDID or
// exact name), else a booted iPhone, else the iPhone on the newest iOS
// runtime.
func pickSimulator(listing []byte, want string) (string, error) {
	var parsed struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal(listing, &parsed); err != nil {
		return "", fmt.Errorf("read simctl's device list: %w", err)
	}
	var all []simDevice
	for rt, devs := range parsed.Devices {
		ver, ok := iosRuntimeVersion(rt)
		if !ok {
			continue // watchOS, tvOS, visionOS
		}
		for _, d := range devs {
			if d.IsAvailable || d.State != "" {
				d.runtime = ver
				all = append(all, d)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if c := compareVersions(all[i].runtime, all[j].runtime); c != 0 {
			return c > 0
		}
		return all[i].Name < all[j].Name
	})
	if want != "" {
		for _, d := range all {
			if d.UDID == want || d.Name == want {
				return d.UDID, nil
			}
		}
		return "", fmt.Errorf("no available iOS simulator named %q (list them: xcrun simctl list devices available)", want)
	}
	for _, d := range all {
		if d.State == "Booted" && strings.HasPrefix(d.Name, "iPhone") {
			return d.UDID, nil
		}
	}
	for _, d := range all {
		if strings.HasPrefix(d.Name, "iPhone") {
			return d.UDID, nil
		}
	}
	return "", fmt.Errorf("no iPhone simulator is installed: Xcode > Settings > Components adds an iOS runtime")
}

// iosRuntimeVersion reads "com.apple.CoreSimulator.SimRuntime.iOS-18-2".
func iosRuntimeVersion(key string) ([]int, bool) {
	_, rest, ok := strings.Cut(key, ".SimRuntime.iOS-")
	if !ok {
		return nil, false
	}
	var ver []int
	for _, p := range strings.Split(rest, "-") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		ver = append(ver, n)
	}
	return ver, true
}

func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

type runOpts struct {
	device     string
	screenshot string
	wait       time.Duration
}
