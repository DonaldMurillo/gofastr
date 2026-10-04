//go:build windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsVersionParts(t *testing.T) {
	parts, err := windowsVersionParts("1.27.4.9")
	if err != nil {
		t.Fatal(err)
	}
	if want := [4]uint16{1, 27, 4, 9}; parts != want {
		t.Fatalf("windowsVersionParts() = %v, want %v", parts, want)
	}
	for _, version := range []string{"bad", "1.65536", "1.2.3.4.5", strings.Repeat("1", 257)} {
		if _, err := windowsVersionParts(version); err == nil {
			t.Errorf("windowsVersionParts(%q) succeeded", version)
		}
	}
}

func TestWindowsDesktopLinkerFlagsUseGUIAndEmbedScheme(t *testing.T) {
	flags := windowsDesktopLinkerFlags("gofastr-notes")
	for _, want := range []string{
		"-H windowsgui",
		"-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr-notes",
	} {
		if !strings.Contains(flags, want) {
			t.Errorf("linker flags %q do not include %q", flags, want)
		}
	}
	if flags := windowsDesktopLinkerFlags(""); strings.Contains(flags, "builtDeepLinkScheme=") {
		t.Fatalf("linker flags without a scheme still embed one: %q", flags)
	}
}

func TestWindowsIconResourcesBuildGroupReferences(t *testing.T) {
	pngBytes, _ := loadIconSource("")
	ico, err := buildWindowsICO(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := windowsIconResources(ico)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 5 {
		t.Fatalf("icon resources = %d, want four image resources and one group", len(resources))
	}
	for i, resource := range resources[:4] {
		if resource.Type != 3 || resource.ID != uint16(i+1) || resource.Language != 0 || len(resource.Data) == 0 {
			t.Errorf("icon resource %d = %+v", i, resource)
		}
	}
	group := resources[4]
	if group.Type != 14 || group.ID != 1 || group.Language != 0 || binary.LittleEndian.Uint16(group.Data[4:6]) != 4 {
		t.Fatalf("group icon resource = %+v", group)
	}
	for i := 0; i < 4; i++ {
		entry := 6 + i*14
		if got := binary.LittleEndian.Uint16(group.Data[entry+12 : entry+14]); got != uint16(i+1) {
			t.Errorf("group frame %d points at icon %d, want %d", i, got, i+1)
		}
	}
	if _, err := windowsIconResources([]byte{0, 0, 1, 0, 1}); err == nil {
		t.Fatal("windowsIconResources accepted a truncated directory")
	}
}

func TestWriteWindowsVersionResource(t *testing.T) {
	currentExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(currentExe)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "version-resource-test.exe")
	if err := os.WriteFile(target, data, 0o700); err != nil {
		t.Fatal(err)
	}
	info := windowsDesktopVersion{Name: "Version Test", ID: "dev.gofastr.version-test", Version: "1.27.4"}
	if err := writeWindowsVersionResource(target, info); err != nil {
		t.Fatal(err)
	}
	iconPNG, _ := loadIconSource("")
	icon, err := buildWindowsICO(iconPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsIconResource(target, icon); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		`\StringFileInfo\040904B0\ProductName`:      info.Name,
		`\StringFileInfo\040904B0\FileDescription`:  info.Name,
		`\StringFileInfo\040904B0\FileVersion`:      info.Version,
		`\StringFileInfo\040904B0\ProductVersion`:   info.Version,
		`\StringFileInfo\040904B0\OriginalFilename`: info.Name + ".exe",
		`\StringFileInfo\040904B0\Comments`:         "Application ID: " + info.ID,
	} {
		if got := readWindowsVersionString(t, target, query); got != want {
			t.Errorf("version resource %s = %q, want %q", query, got, want)
		}
	}
}

func readWindowsVersionString(t *testing.T, path, query string) string {
	t.Helper()
	versionDLL := syscall.NewLazyDLL("version.dll")
	sizeProc := versionDLL.NewProc("GetFileVersionInfoSizeW")
	infoProc := versionDLL.NewProc("GetFileVersionInfoW")
	queryProc := versionDLL.NewProc("VerQueryValueW")
	for _, proc := range []*syscall.LazyProc{sizeProc, infoProc, queryProc} {
		if err := proc.Find(); err != nil {
			t.Fatal(err)
		}
	}
	widePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	var handle uint32
	size, _, callErr := sizeProc.Call(uintptr(unsafe.Pointer(widePath)), uintptr(unsafe.Pointer(&handle)))
	runtime.KeepAlive(widePath)
	if size == 0 {
		t.Fatalf("GetFileVersionInfoSizeW: %v", windowsResourceCallError("GetFileVersionInfoSizeW", callErr))
	}
	buffer := make([]byte, int(size))
	ok, _, callErr := infoProc.Call(uintptr(unsafe.Pointer(widePath)), 0, size, uintptr(unsafe.Pointer(&buffer[0])))
	runtime.KeepAlive(widePath)
	runtime.KeepAlive(buffer)
	if ok == 0 {
		t.Fatalf("GetFileVersionInfoW: %v", windowsResourceCallError("GetFileVersionInfoW", callErr))
	}
	wideQuery, err := syscall.UTF16PtrFromString(query)
	if err != nil {
		t.Fatal(err)
	}
	var value uintptr
	var length uint32
	ok, _, callErr = queryProc.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(wideQuery)),
		uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&length)))
	runtime.KeepAlive(wideQuery)
	runtime.KeepAlive(buffer)
	if ok == 0 || value == 0 || length == 0 {
		t.Fatalf("VerQueryValueW(%q): %v", query, windowsResourceCallError("VerQueryValueW", callErr))
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(value)), int(length)))
}

func TestWindowsVersionResourceErrorIsNamed(t *testing.T) {
	if err := writeWindowsVersionResource(filepath.Join(t.TempDir(), "missing.exe"), windowsDesktopVersion{
		Name: "Missing", ID: "dev.gofastr.missing", Version: "1.0.0",
	}); err == nil {
		t.Fatal("writeWindowsVersionResource succeeded for a missing executable")
	}
}
