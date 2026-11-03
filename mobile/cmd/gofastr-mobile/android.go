package main

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Android build never touches Gradle or Android Studio. It drives the
// SDK's command-line tools directly:
//
//	gomobile bind -> <pkg>.aar (Java bindings + libgojni.so per ABI)
//	javac         -> the generated MainActivity.java
//	d8            -> classes.dex
//	aapt2 link    -> a base APK from the generated AndroidManifest.xml
//	(in Go)       -> add classes.dex and lib/<abi>/libgojni.so
//	zipalign, apksigner (debug key) -> an installable APK
//	adb / emulator -> boot, install, launch, screenshot

// androidSDK is what the build needs from the Android SDK. In a dry run
// a missing piece becomes a placeholder so the plan still prints.
type androidSDK struct {
	root, buildTools, androidJar, ndk, adb, emulator string
	api                                              int
	javac, keytool                                   string
}

func findAndroidSDK(dry bool) (androidSDK, error) {
	var s androidSDK
	var missing []string
	s.root = firstEnv("ANDROID_HOME", "ANDROID_SDK_ROOT")
	if s.root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			for _, p := range []string{filepath.Join(home, "Library", "Android", "sdk"), filepath.Join(home, "Android", "Sdk")} {
				if isDir(p) {
					s.root = p
					break
				}
			}
		}
	}
	if s.root == "" {
		missing = append(missing, "the Android SDK (set ANDROID_HOME)")
		s.root = "$ANDROID_HOME"
	}
	if v := highestVersionDir(filepath.Join(s.root, "build-tools")); v != "" {
		s.buildTools = filepath.Join(s.root, "build-tools", v)
	} else {
		missing = append(missing, "build-tools")
		s.buildTools = filepath.Join(s.root, "build-tools", "<version>")
	}
	if api := highestPlatform(filepath.Join(s.root, "platforms")); api > 0 {
		s.api = api
		s.androidJar = filepath.Join(s.root, "platforms", "android-"+strconv.Itoa(api), "android.jar")
	} else {
		missing = append(missing, "an SDK platform (platforms;android-NN)")
		s.api = minSDK
		s.androidJar = filepath.Join(s.root, "platforms", "android-<api>", "android.jar")
	}
	s.ndk = os.Getenv("ANDROID_NDK_HOME")
	if s.ndk == "" {
		if v := highestVersionDir(filepath.Join(s.root, "ndk")); v != "" {
			s.ndk = filepath.Join(s.root, "ndk", v)
		}
	}
	if s.ndk == "" {
		missing = append(missing, "the NDK (ndk;<version>)")
		s.ndk = filepath.Join(s.root, "ndk", "<version>")
	}
	s.adb = filepath.Join(s.root, "platform-tools", "adb")
	s.emulator = filepath.Join(s.root, "emulator", "emulator")
	s.javac = jdkTool("javac")
	s.keytool = jdkTool("keytool")
	if s.javac == "" || s.keytool == "" {
		missing = append(missing, "a JDK (javac and keytool on PATH or under $JAVA_HOME)")
		s.javac, s.keytool = "javac", "keytool"
	}
	if len(missing) > 0 && !dry {
		return s, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return s, nil
}

func (s androidSDK) tool(name string) string { return filepath.Join(s.buildTools, name) }

func buildAndroid(r *runner, a app, out string) (string, error) {
	sdk, err := findAndroidSDK(r.dryRun)
	if err != nil {
		return "", fmt.Errorf("%w (run: gofastr-mobile doctor --platform android-emu)", err)
	}
	a = a.forAndroid(sdk.api)
	work := filepath.Join(out, "work")
	bin := filepath.Join(work, "bin")
	if err := bootstrapGomobile(r, bin); err != nil {
		return "", err
	}
	r.env = append(r.env, "ANDROID_HOME="+sdk.root, "ANDROID_NDK_HOME="+sdk.ndk)

	aar := filepath.Join(work, a.PkgName+".aar")
	if err := r.run("bind the Go app as an Android library",
		filepath.Join(bin, "gomobile"), "bind", "-target=android/arm64,android/amd64",
		"-androidapi", strconv.Itoa(a.MinSDK), "-o", aar, a.PkgPath); err != nil {
		return "", err
	}
	unpacked := filepath.Join(work, "aar")
	if err := r.do("unpack the bindings and native libraries", func() error { return unpackAAR(aar, unpacked) }); err != nil {
		return "", err
	}
	classesJar := filepath.Join(unpacked, "classes.jar")

	src := filepath.Join(work, "android", "src", filepath.FromSlash(strings.ReplaceAll(a.ID, ".", "/")), "MainActivity.java")
	manifest := filepath.Join(work, "android", "AndroidManifest.xml")
	if err := r.do("generate MainActivity.java and AndroidManifest.xml", func() error {
		if err := writeRendered("templates/android/MainActivity.java.tmpl", src, a); err != nil {
			return err
		}
		return writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a)
	}); err != nil {
		return "", err
	}
	classes := filepath.Join(work, "android", "classes")
	if err := r.do("clear old classes", func() error { return os.RemoveAll(classes) }); err != nil {
		return "", err
	}
	if err := r.run("compile the Java host", sdk.javac, "--release", "11", "-nowarn",
		"-classpath", sdk.androidJar+string(os.PathListSeparator)+classesJar,
		"-d", classes, src); err != nil {
		return "", err
	}
	classFiles := []string{filepath.Join(classes, "<class files>")}
	if !r.dryRun {
		if classFiles, err = listFiles(classes, ".class"); err != nil {
			return "", err
		}
	}
	dex := filepath.Join(work, "android", "dex")
	if err := r.do("clear old dex output", func() error {
		if err := os.RemoveAll(dex); err != nil {
			return err
		}
		return os.MkdirAll(dex, 0o700)
	}); err != nil {
		return "", err
	}
	d8Args := append([]string{"--release", "--min-api", strconv.Itoa(a.MinSDK), "--lib", sdk.androidJar, "--output", dex}, classFiles...)
	if err := r.run("convert the Java classes to dex", sdk.tool("d8"), append(d8Args, classesJar)...); err != nil {
		return "", err
	}
	base := filepath.Join(work, "android", "base.apk")
	if err := r.run("link the manifest into a base APK", sdk.tool("aapt2"), "link",
		"-o", base, "--manifest", manifest, "-I", sdk.androidJar,
		"--min-sdk-version", strconv.Itoa(a.MinSDK), "--target-sdk-version", strconv.Itoa(a.TargetSDK),
		"--version-code", strconv.Itoa(a.VersionCode), "--version-name", a.Version); err != nil {
		return "", err
	}
	unsigned := filepath.Join(work, "android", "unsigned.apk")
	if err := r.do("add classes.dex and the native libraries", func() error {
		return addToAPK(base, unsigned, filepath.Join(dex, "classes.dex"), filepath.Join(unpacked, "jni"))
	}); err != nil {
		return "", err
	}
	aligned := filepath.Join(work, "android", "aligned.apk")
	if err := r.run("align the APK", sdk.tool("zipalign"), "-p", "-f", "4", unsigned, aligned); err != nil {
		return "", err
	}
	ks, err := debugKeystore(r, sdk, work)
	if err != nil {
		return "", err
	}
	apk := filepath.Join(out, a.Exe+".apk")
	if err := r.run("sign the APK with the debug key", sdk.tool("apksigner"), "sign",
		"--ks", ks, "--ks-pass", "pass:android", "--key-pass", "pass:android", "--ks-key-alias", "androiddebugkey",
		"--out", apk, aligned); err != nil {
		return "", err
	}
	return apk, nil
}

func runAndroid(r *runner, a app, apk string, opts runOpts) error {
	sdk, err := findAndroidSDK(r.dryRun)
	if err != nil {
		return err
	}
	serial, err := androidDevice(r, sdk, opts.device)
	if err != nil {
		return err
	}
	if err := r.run("install the app", sdk.adb, "-s", serial, "install", "-r", apk); err != nil {
		return err
	}
	if err := r.run("launch the app", sdk.adb, "-s", serial, "shell", "am", "start", "-W", "-n", a.ID+"/.MainActivity"); err != nil {
		return err
	}
	if opts.screenshot != "" {
		r.sleep(opts.wait, "for the first page")
		png, err := r.outputBytes("take a screenshot", nil, sdk.adb, "-s", serial, "exec-out", "screencap", "-p")
		if err != nil {
			return err
		}
		if err := r.do("write "+opts.screenshot, func() error { return writeOwnerOnly(opts.screenshot, png) }); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "\nRunning on %s.\n  Logs:     %s -s %s logcat -s gofastr-mobile GoLog chromium\n  DevTools: chrome://inspect in desktop Chrome\n", serial, sdk.adb, serial)
	return nil
}

// androidDevice returns the serial of a running device or emulator,
// booting an AVD when none is attached.
func androidDevice(r *runner, sdk androidSDK, want string) (string, error) {
	r.tolerate("start the adb server", sdk.adb, "start-server")
	listing, err := r.output("list attached devices", "", sdk.adb, "devices")
	if err != nil {
		return "", err
	}
	if serials := parseADBDevices(listing); len(serials) > 0 && !r.dryRun {
		if want == "" {
			return serials[0], nil
		}
		for _, s := range serials {
			if s == want {
				return s, nil
			}
		}
	}
	avd := want
	if avd == "" {
		avds, err := r.output("list emulator images (AVDs)", "<avd>", sdk.emulator, "-list-avds")
		if err != nil {
			return "", err
		}
		names := strings.Fields(avds)
		if len(names) == 0 {
			return "", errors.New("no device attached and no AVD to boot: create one in Android Studio's Device Manager or with avdmanager (see gofastr-mobile doctor)")
		}
		avd = names[0]
	}
	if err := r.do("boot the emulator "+avd+" in the background", func() error {
		cmd := exec.Command(sdk.emulator, "-avd", avd, "-netdelay", "none", "-netspeed", "full")
		cmd.Env = append(os.Environ(), r.env...)
		// Detached: the emulator outlives this command, like Simulator.app.
		return cmd.Start()
	}); err != nil {
		return "", err
	}
	if err := r.run("wait for the emulator to attach", sdk.adb, "wait-for-device"); err != nil {
		return "", err
	}
	if !r.dryRun {
		deadline := time.Now().Add(4 * time.Minute)
		for {
			// adb answers an error while the device is still coming up;
			// that is "not yet", not a verdict.
			done, err := r.quietOutput(sdk.adb, "shell", "getprop", "sys.boot_completed")
			if err == nil && strings.TrimSpace(done) == "1" {
				break
			}
			if time.Now().After(deadline) {
				return "", errors.New("the emulator did not finish booting within 4 minutes")
			}
			time.Sleep(2 * time.Second)
		}
	}
	listing, err = r.output("find the emulator's serial", "emulator-5554", sdk.adb, "devices")
	if err != nil {
		return "", err
	}
	if r.dryRun {
		return "emulator-5554", nil
	}
	if serials := parseADBDevices(listing); len(serials) > 0 {
		return serials[0], nil
	}
	return "", errors.New("the emulator booted but adb lists no device")
}

// quietOutput runs a polling command without a step header.
func (r *runner) quietOutput(name string, args ...string) (string, error) {
	cmd := r.command(name, args...)
	b, err := cmd.Output()
	return string(b), err
}

// parseADBDevices reads `adb devices`: the serials in state "device".
func parseADBDevices(out string) []string {
	var serials []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == "device" {
			serials = append(serials, f[0])
		}
	}
	return serials
}

// debugKeystore is the SDK's usual ~/.android/debug.keystore, or one
// minted in the work dir the first time.
func debugKeystore(r *runner, sdk androidSDK, work string) (string, error) {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".android", "debug.keystore")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p := filepath.Join(work, "debug.keystore")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	err := r.run("create a debug signing key", sdk.keytool, "-genkeypair", "-keystore", p,
		"-storepass", "android", "-keypass", "android", "-alias", "androiddebugkey",
		"-keyalg", "RSA", "-keysize", "2048", "-validity", "10000",
		"-dname", "CN=Android Debug,O=Android,C=US")
	return p, err
}

// unpackAAR extracts classes.jar and jni/<abi>/*.so from gomobile's aar.
func unpackAAR(aar, dst string) error {
	zr, err := zip.OpenReader(aar)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	root, err := mkRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if name != "classes.jar" && !(strings.HasPrefix(name, "jni/") && strings.HasSuffix(name, ".so")) {
			continue
		}
		if err := copyZipEntry(root, f, name); err != nil {
			return err
		}
	}
	return nil
}

// mkRoot creates dst and opens it as an os.Root: entry names from the
// archive are written through it, so none can escape.
func mkRoot(dst string) (*os.Root, error) {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(dst)
}

func copyZipEntry(root *os.Root, f *zip.File, name string) error {
	if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// A native library is tens of MB; the cap only refuses a runaway entry.
	if _, err := io.Copy(w, io.LimitReader(rc, 1<<30)); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// addToAPK copies base (aapt2's output) to dst and adds classes.dex and
// every lib/<abi>/*.so from jniDir.
func addToAPK(base, dst, dexFile, jniDir string) error {
	zr, err := zip.OpenReader(base)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			out.Close()
			return err
		}
	}
	add := func(name, src string) error {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	if err := add("classes.dex", dexFile); err != nil {
		out.Close()
		return err
	}
	libs, err := listFiles(jniDir, ".so")
	if err != nil {
		out.Close()
		return err
	}
	if len(libs) == 0 {
		out.Close()
		return fmt.Errorf("no native libraries under %s: gomobile bind wrote no jni/<abi>/*.so", jniDir)
	}
	for _, lib := range libs {
		rel, err := filepath.Rel(jniDir, lib)
		if err != nil {
			out.Close()
			return err
		}
		if err := add("lib/"+filepath.ToSlash(rel), lib); err != nil {
			out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func listFiles(dir, suffix string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, suffix) {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func highestVersionDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestVer := "", []int(nil)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var ver []int
		ok := true
		for _, p := range strings.Split(e.Name(), ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				ok = false
				break
			}
			ver = append(ver, n)
		}
		if ok && (best == "" || compareVersions(ver, bestVer) > 0) {
			best, bestVer = e.Name(), ver
		}
	}
	return best
}

func highestPlatform(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	best := 0
	for _, e := range entries {
		n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "android-"))
		if err == nil && n > best && fileExists(filepath.Join(dir, e.Name(), "android.jar")) {
			best = n
		}
	}
	return best
}

func jdkTool(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		if p := filepath.Join(jh, "bin", name); fileExists(p) {
			return p
		}
	}
	p, _ := exec.LookPath(name)
	return p
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
