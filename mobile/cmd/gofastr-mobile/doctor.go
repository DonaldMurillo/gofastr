package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type check struct {
	name string
	ok   bool
	got  string
	fix  string
}

// doctor checks the toolchain for one platform (or both) and prints what
// to install for anything missing. It only reads.
func doctor(r *runner, platform string) int {
	var checks []check
	add := func(c check) { checks = append(checks, c) }

	goVer, err := r.quietOutput("go", "version")
	add(check{name: "Go toolchain", ok: err == nil, got: strings.TrimSpace(goVer), fix: "install Go: https://go.dev/dl/"})
	mob, err := r.quietOutput("go", "list", "-m", "golang.org/x/mobile")
	add(check{name: "golang.org/x/mobile in this module", ok: err == nil, got: strings.TrimSpace(mob),
		fix: "go get -tool golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind"})

	if platform == "" || platform == "ios-sim" {
		onMac := runtime.GOOS == "darwin"
		add(check{name: "macOS (the iOS SDK only exists there)", ok: onMac, got: runtime.GOOS, fix: "iOS builds need a Mac"})
		if onMac {
			dev, err := r.quietOutput("xcode-select", "-p")
			dev = strings.TrimSpace(dev)
			full := err == nil && strings.Contains(dev, ".app/")
			add(check{name: "Xcode selected (not just the Command Line Tools)", ok: full, got: dev,
				fix: "install Xcode from the App Store, then: sudo xcode-select -s /Applications/Xcode.app && xcodebuild -runFirstLaunch"})
			sdk, err := r.quietOutput("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
			add(check{name: "iOS simulator SDK", ok: err == nil, got: strings.TrimSpace(sdk), fix: "open Xcode once and accept the license, or run xcodebuild -runFirstLaunch"})
			_, err = r.quietOutput("xcrun", "--sdk", "iphonesimulator", "--find", "swiftc")
			add(check{name: "swiftc", ok: err == nil, fix: "comes with Xcode"})
			listing, err := r.quietOutput("xcrun", "simctl", "list", "devices", "available", "--json")
			udid := ""
			if err == nil {
				udid, err = pickSimulator([]byte(listing), "")
			}
			add(check{name: "an iPhone simulator", ok: err == nil, got: udid, fix: "Xcode > Settings > Components: install an iOS simulator runtime"})
			_, err = exec.LookPath("codesign")
			add(check{name: "codesign", ok: err == nil, fix: "comes with Xcode"})
		}
	}

	if platform == "" || platform == "android-emu" {
		sdk, err := findAndroidSDK(false)
		missing := ""
		if err != nil {
			missing = err.Error()
		}
		add(check{name: "Android SDK, build-tools, platform, NDK, JDK", ok: err == nil, got: firstNonEmpty(missing, sdk.root),
			fix: androidFix})
		if err == nil {
			for _, t := range []string{"d8", "aapt2", "zipalign", "apksigner"} {
				add(check{name: "build-tools " + t, ok: fileExists(sdk.tool(t)), got: sdk.tool(t), fix: androidFix})
			}
			add(check{name: "adb", ok: fileExists(sdk.adb), got: sdk.adb, fix: "sdkmanager \"platform-tools\""})
			hasEmu := fileExists(sdk.emulator)
			add(check{name: "emulator", ok: hasEmu, got: sdk.emulator, fix: "sdkmanager \"emulator\""})
			if hasEmu {
				avds, err := r.quietOutput(sdk.emulator, "-list-avds")
				names := strings.Fields(avds)
				add(check{name: "an Android virtual device (AVD)", ok: err == nil && len(names) > 0, got: strings.Join(names, ", "),
					fix: "Android Studio > Device Manager > Create device, or: avdmanager create avd -n gofastr -k \"system-images;android-35;google_apis;" + avdABI() + "\""})
			}
			jv, err := r.quietOutput(sdk.javac, "-version")
			add(check{name: "javac", ok: err == nil, got: strings.TrimSpace(jv), fix: "install a JDK 17+ (Android Studio bundles one: export JAVA_HOME=\"/Applications/Android Studio.app/Contents/jbr/Contents/Home\")"})
		}
	}

	failed := 0
	for _, c := range checks {
		mark := "ok  "
		if !c.ok {
			mark = "MISS"
			failed++
		}
		line := fmt.Sprintf("[%s] %s", mark, c.name)
		if c.got != "" {
			line += ": " + c.got
		}
		fmt.Fprintln(r.out, line)
		if !c.ok && c.fix != "" {
			fmt.Fprintf(r.out, "       fix: %s\n", c.fix)
		}
	}
	if failed > 0 {
		fmt.Fprintf(r.out, "\n%d missing.\n", failed)
		return 1
	}
	fmt.Fprintln(r.out, "\nEverything is in place.")
	return 0
}

var androidFix = "Android Studio > Settings > Languages & Frameworks > Android SDK: install an SDK Platform, and under SDK Tools: Build-Tools, NDK (Side by side), Command-line Tools, Emulator, Platform-Tools. Or with sdkmanager: sdkmanager \"platforms;android-35\" \"build-tools;35.0.0\" \"ndk;27.2.12479018\" \"platform-tools\" \"emulator\" \"system-images;android-35;google_apis;" + avdABI() + "\" (versions are examples). Then export ANDROID_HOME=" + filepath.Join("$HOME", "Library", "Android", "sdk")

func avdABI() string {
	if runtime.GOARCH == "arm64" {
		return "arm64-v8a"
	}
	return "x86_64"
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
